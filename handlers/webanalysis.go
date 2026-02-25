package handlers

import (
	"context"
	"errors"
	"fmt"
	stdhtml "html"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ashwinsekaran/webpage-analysis-go/domain"
	"github.com/julienschmidt/httprouter"
)

const (
	maxBodyBytes       = 4 << 20
	userAgent          = "webpage-analysis-go/1.0"
	defaultTimeout     = 20 * time.Second
	defaultLinkTimeout = 5 * time.Second
)

var (
	doctypeRegexp  = regexp.MustCompile(`(?is)<!doctype\s+([^>]+)>`)
	titleRegexp    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	formRegexp     = regexp.MustCompile(`(?is)<form\b[^>]*>(.*?)</form>`)
	passwordRegexp = regexp.MustCompile(`(?is)<input\b[^>]*type\s*=\s*(?:"password"|'password'|password)(?:\s|>|/)`)
	anchorRegexp   = regexp.MustCompile(`(?is)<a\b[^>]*\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	tagRegexp      = regexp.MustCompile(`(?is)<[^>]+>`)
)

type WebAnalysisHandler struct {
	templates       *template.Template
	clientTimeout   time.Duration
	linkTimeout     time.Duration
	maxCheckedLinks int
}

type WebAnalysisViewData struct {
	InputURL string
	Result   *domain.PageAnalysisResult
	Error    *domain.PageAnalysisError
}

type discoveredLink struct {
	URL      string
	Internal bool
}

func NewWebAnalysisHandler(templateDir string, clientTimeout, linkTimeout time.Duration, maxCheckedLinks int) (*WebAnalysisHandler, error) {
	if clientTimeout <= 0 {
		clientTimeout = defaultTimeout
	}
	if linkTimeout <= 0 {
		linkTimeout = defaultLinkTimeout
	}
	if maxCheckedLinks <= 0 {
		maxCheckedLinks = 150
	}

	t, err := template.ParseFiles(
		templateDir+"/layout.gohtml",
		templateDir+"/header.gohtml",
		templateDir+"/content.gohtml",
		templateDir+"/footer.gohtml",
	)
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	return &WebAnalysisHandler{
		templates:       t,
		clientTimeout:   clientTimeout,
		linkTimeout:     linkTimeout,
		maxCheckedLinks: maxCheckedLinks,
	}, nil
}

func (h *WebAnalysisHandler) Get(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	h.render(w, WebAnalysisViewData{})
}

func (h *WebAnalysisHandler) Post(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if err := r.ParseForm(); err != nil {
		h.render(w, WebAnalysisViewData{Error: domain.NewPageAnalysisError(http.StatusBadRequest, "Invalid form submission.")})
		return
	}

	inputURL := strings.TrimSpace(r.FormValue("url"))
	view := WebAnalysisViewData{InputURL: inputURL}

	parsedURL, err := validateURL(inputURL)
	if err != nil {
		view.Error = domain.NewPageAnalysisError(http.StatusBadRequest, err.Error())
		h.render(w, view)
		return
	}

	result, analyzeErr := h.analyze(r.Context(), parsedURL)
	view.Result = result
	view.Error = analyzeErr
	h.render(w, view)
}

func (h *WebAnalysisHandler) render(w http.ResponseWriter, data WebAnalysisViewData) {
	w.Header().Set(HeaderContentType, "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, "Unable to render page", http.StatusInternalServerError)
	}
}

func validateURL(rawURL string) (*url.URL, error) {
	if rawURL == "" {
		return nil, errors.New("Please enter a URL to analyze")
	}

	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, errors.New("Enter a valid URL, for example https://example.com")
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, errors.New("Only http:// and https:// URLs are supported")
	}

	if parsed.Host == "" || strings.Contains(parsed.Host, " ") {
		return nil, errors.New("URL must include a valid host, for example https://example.com")
	}

	return parsed, nil
}

func (h *WebAnalysisHandler) analyze(parentCtx context.Context, parsedURL *url.URL) (*domain.PageAnalysisResult, *domain.PageAnalysisError) {
	ctx, cancel := context.WithTimeout(parentCtx, h.clientTimeout)
	defer cancel()

	redirects := 0
	client := &http.Client{
		Timeout: h.clientTimeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			redirects = len(via)
			if len(via) > 10 {
				return errors.New("too many redirects")
			}
			return nil
		},
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, domain.NewPageAnalysisError(http.StatusBadRequest, "Could not create request for URL")
	}
	request.Header.Set("User-Agent", userAgent)

	startedAt := time.Now()
	response, err := client.Do(request)
	elapsed := time.Since(startedAt)
	if err != nil {
		return nil, mapRequestError(err)
	}
	defer response.Body.Close()

	bodyBytes, readErr := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if readErr != nil {
		return nil, domain.NewPageAnalysisError(http.StatusBadGateway, fmt.Sprintf("Failed to read response body: %v", readErr))
	}

	headings, rawLinks, hasLoginForm, htmlVersion, title := analyzeHTML(bodyBytes)
	links := normalizeLinks(response.Request.URL, rawLinks)
	inaccessible, checked, skipped := h.countInaccessibleLinks(parentCtx, links)

	result := &domain.PageAnalysisResult{
		RequestedURL:       parsedURL.String(),
		FinalURL:           response.Request.URL.String(),
		StatusCode:         response.StatusCode,
		StatusText:         response.Status,
		HTMLVersion:        htmlVersion,
		PageTitle:          title,
		HeadingCounts:      headings,
		InternalLinks:      countInternalLinks(links),
		ExternalLinks:      countExternalLinks(links),
		TotalLinks:         len(links),
		InaccessibleLinks:  inaccessible,
		CheckedLinks:       checked,
		SkippedLinkChecks:  skipped,
		HasLoginForm:       hasLoginForm,
		ResponseTime:       elapsed.Round(time.Millisecond).String(),
		ContentType:        response.Header.Get("Content-Type"),
		ContentLength:      response.ContentLength,
		ServerHeader:       response.Header.Get("Server"),
		RedirectCount:      redirects,
		AnalysisFinishedAt: time.Now().Format(time.RFC1123),
	}
	result.ApplyStatusPresentation()

	if response.StatusCode >= http.StatusBadRequest {
		return result, domain.NewPageAnalysisError(response.StatusCode, fmt.Sprintf("Target page responded with %s", response.Status))
	}

	return result, nil
}

func mapRequestError(err error) *domain.PageAnalysisError {
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.NewPageAnalysisError(http.StatusGatewayTimeout, "Timed out while connecting to the URL")
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if opErr, ok := urlErr.Err.(*net.OpError); ok {
			return domain.NewPageAnalysisError(http.StatusBadGateway, fmt.Sprintf("Network error while reaching URL: %v", opErr))
		}
		return domain.NewPageAnalysisError(http.StatusBadGateway, fmt.Sprintf("Failed to reach URL: %v", urlErr.Err))
	}

	return domain.NewPageAnalysisError(http.StatusBadGateway, fmt.Sprintf("Failed to reach URL: %v", err))
}

func analyzeHTML(body []byte) ([]domain.HeadingCount, []discoveredLink, bool, string, string) {
	htmlBody := string(body)
	lowerBody := strings.ToLower(htmlBody)

	headingCounts := make([]domain.HeadingCount, 0, 6)
	for level := 1; level <= 6; level++ {
		pattern := regexp.MustCompile(fmt.Sprintf(`(?is)<h%d(?:\s[^>]*)?>`, level))
		headingCounts = append(headingCounts, domain.HeadingCount{Level: level, Count: len(pattern.FindAllStringIndex(htmlBody, -1))})
	}

	htmlVersion := "Unknown"
	if doctypeMatch := doctypeRegexp.FindStringSubmatch(htmlBody); len(doctypeMatch) > 1 {
		doctype := strings.TrimSpace(strings.ToLower(doctypeMatch[1]))
		if doctype == "html" {
			htmlVersion = "HTML5"
		} else {
			htmlVersion = strings.TrimSpace(doctypeMatch[1])
		}
	}

	title := "(No title found)"
	if titleMatch := titleRegexp.FindStringSubmatch(htmlBody); len(titleMatch) > 1 {
		cleanTitle := strings.TrimSpace(tagRegexp.ReplaceAllString(titleMatch[1], ""))
		if cleanTitle != "" {
			title = stdhtml.UnescapeString(cleanTitle)
		}
	}

	rawLinks := make([]discoveredLink, 0)
	for _, match := range anchorRegexp.FindAllStringSubmatch(htmlBody, -1) {
		href := ""
		for i := 1; i <= 3; i++ {
			if strings.TrimSpace(match[i]) != "" {
				href = strings.TrimSpace(match[i])
				break
			}
		}
		if href != "" {
			rawLinks = append(rawLinks, discoveredLink{URL: href})
		}
	}

	hasLoginForm := false
	for _, form := range formRegexp.FindAllString(lowerBody, -1) {
		if passwordRegexp.MatchString(form) {
			hasLoginForm = true
			break
		}
	}

	return headingCounts, rawLinks, hasLoginForm, htmlVersion, title
}

func countInternalLinks(links []discoveredLink) int {
	count := 0
	for _, link := range links {
		if link.Internal {
			count++
		}
	}
	return count
}

func countExternalLinks(links []discoveredLink) int {
	count := 0
	for _, link := range links {
		if !link.Internal {
			count++
		}
	}
	return count
}

func normalizeLinks(baseURL *url.URL, links []discoveredLink) []discoveredLink {
	normalized := make([]discoveredLink, 0, len(links))
	for _, link := range links {
		href := strings.TrimSpace(link.URL)
		if href == "" || strings.HasPrefix(href, "#") {
			continue
		}
		lowerHref := strings.ToLower(href)
		if strings.HasPrefix(lowerHref, "javascript:") || strings.HasPrefix(lowerHref, "mailto:") || strings.HasPrefix(lowerHref, "tel:") {
			continue
		}

		parsed, err := url.Parse(href)
		if err != nil {
			continue
		}
		resolved := baseURL.ResolveReference(parsed)
		scheme := strings.ToLower(resolved.Scheme)
		if scheme != "http" && scheme != "https" {
			continue
		}

		normalized = append(normalized, discoveredLink{
			URL:      resolved.String(),
			Internal: strings.EqualFold(resolved.Hostname(), baseURL.Hostname()),
		})
	}
	return normalized
}

func (h *WebAnalysisHandler) countInaccessibleLinks(parentCtx context.Context, links []discoveredLink) (int, int, int) {
	if len(links) == 0 {
		return 0, 0, 0
	}

	seen := make(map[string]struct{}, len(links))
	unique := make([]string, 0, len(links))
	for _, link := range links {
		if _, ok := seen[link.URL]; ok {
			continue
		}
		seen[link.URL] = struct{}{}
		unique = append(unique, link.URL)
	}

	slices.Sort(unique)
	checkedTargets := unique
	skipped := 0
	if len(unique) > h.maxCheckedLinks {
		checkedTargets = unique[:h.maxCheckedLinks]
		skipped = len(unique) - h.maxCheckedLinks
	}

	ctx, cancel := context.WithTimeout(parentCtx, h.clientTimeout)
	defer cancel()

	const workers = 8
	type result struct{ inaccessible bool }

	jobs := make(chan string)
	results := make(chan result)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range jobs {
				results <- result{inaccessible: h.isLinkInaccessible(ctx, target)}
			}
		}()
	}

	go func() {
		for _, target := range checkedTargets {
			jobs <- target
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	inaccessible := 0
	for res := range results {
		if res.inaccessible {
			inaccessible++
		}
	}

	return inaccessible, len(checkedTargets), skipped
}

func (h *WebAnalysisHandler) isLinkInaccessible(parentCtx context.Context, target string) bool {
	ctx, cancel := context.WithTimeout(parentCtx, h.linkTimeout)
	defer cancel()

	client := &http.Client{Timeout: h.linkTimeout}
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return true
	}
	headReq.Header.Set("User-Agent", userAgent)

	headResp, err := client.Do(headReq)
	if err == nil {
		defer headResp.Body.Close()
		if headResp.StatusCode < http.StatusBadRequest {
			return false
		}
		if headResp.StatusCode != http.StatusMethodNotAllowed && headResp.StatusCode != http.StatusNotImplemented {
			return true
		}
	}

	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return true
	}
	getReq.Header.Set("User-Agent", userAgent)
	getReq.Header.Set("Range", "bytes=0-0")

	getResp, err := client.Do(getReq)
	if err != nil {
		return true
	}
	defer getResp.Body.Close()
	return getResp.StatusCode >= http.StatusBadRequest
}
