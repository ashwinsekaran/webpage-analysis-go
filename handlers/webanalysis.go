package handlers

import (
	"context"
	"encoding/json"
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
	// Regexes are intentionally case-insensitive and dotall-enabled because input is raw HTML text.
	doctypeRegexp  = regexp.MustCompile(`(?is)<!doctype\s+([^>]+)>`)
	titleRegexp    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	formRegexp     = regexp.MustCompile(`(?is)<form\b[^>]*>(.*?)</form>`)
	passwordRegexp = regexp.MustCompile(`(?is)<input\b[^>]*type\s*=\s*(?:"password"|'password'|password)(?:\s|>|/)`)
	anchorRegexp   = regexp.MustCompile(`(?is)<a\b[^>]*\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	tagRegexp      = regexp.MustCompile(`(?is)<[^>]+>`)
)

// Analyzer defines the analysis contract used by HTTP and UI handlers.
type Analyzer interface {
	Analyze(ctx context.Context, rawURL string) (*domain.PageAnalysisResult, *domain.PageAnalysisError)
}

// HTTPAnalyzer fetches a webpage and computes analysis metrics from response metadata and HTML.
type HTTPAnalyzer struct {
	clientTimeout   time.Duration
	linkTimeout     time.Duration
	maxCheckedLinks int
}

// WebAnalysisHandler wires template rendering and API endpoints to an Analyzer.
type WebAnalysisHandler struct {
	templates *template.Template
	analyzer  Analyzer
}

// WebAnalysisViewData is the template model used by the HTML page.
type WebAnalysisViewData struct {
	InputURL string
	Result   *domain.PageAnalysisResult
	Error    *domain.PageAnalysisError
}

// APIAnalyzeRequest is the JSON request shape for /api/analyze.
type APIAnalyzeRequest struct {
	URL string `json:"url"`
}

// APIAnalyzeResponse is the JSON response shape for /api/analyze.
type APIAnalyzeResponse struct {
	Result *domain.PageAnalysisResult `json:"result,omitempty"`
	Error  *domain.PageAnalysisError  `json:"error,omitempty"`
}

// discoveredLink is an internal representation of a parsed anchor link.
type discoveredLink struct {
	URL      string
	Internal bool
}

// NewHTTPAnalyzer builds an analyzer with safe defaults for invalid timeout/link limit values.
func NewHTTPAnalyzer(clientTimeout, linkTimeout time.Duration, maxCheckedLinks int) *HTTPAnalyzer {
	if clientTimeout <= 0 {
		clientTimeout = defaultTimeout
	}
	if linkTimeout <= 0 {
		linkTimeout = defaultLinkTimeout
	}
	if maxCheckedLinks <= 0 {
		maxCheckedLinks = 150
	}

	return &HTTPAnalyzer{
		clientTimeout:   clientTimeout,
		linkTimeout:     linkTimeout,
		maxCheckedLinks: maxCheckedLinks,
	}
}

// NewWebAnalysisHandler parses templates and returns a handler backed by the provided analyzer.
func NewWebAnalysisHandler(templateDir string, analyzer Analyzer) (*WebAnalysisHandler, error) {
	if analyzer == nil {
		analyzer = NewHTTPAnalyzer(defaultTimeout, defaultLinkTimeout, 150)
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

	return &WebAnalysisHandler{templates: t, analyzer: analyzer}, nil
}

// Get renders the initial page without analysis results.
func (h *WebAnalysisHandler) Get(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	h.render(w, WebAnalysisViewData{})
}

// Post reads the submitted URL, runs analysis, and renders the result page.
func (h *WebAnalysisHandler) Post(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	if err := r.ParseForm(); err != nil {
		h.render(w, WebAnalysisViewData{Error: domain.NewPageAnalysisError(http.StatusBadRequest, "Invalid form submission.")})
		return
	}

	inputURL := strings.TrimSpace(r.FormValue("url"))
	result, analyzeErr := h.analyzer.Analyze(r.Context(), inputURL)

	view := WebAnalysisViewData{
		InputURL: inputURL,
		Result:   result,
		Error:    analyzeErr,
	}
	h.render(w, view)
}

// AnalyzeAPI accepts either JSON or form data and returns an API-friendly JSON response.
func (h *WebAnalysisHandler) AnalyzeAPI(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	inputURL, reqErr := parseAPIURL(r)
	if reqErr != nil {
		writeJSON(w, reqErr.StatusCode, APIAnalyzeResponse{Error: reqErr})
		return
	}

	result, analyzeErr := h.analyzer.Analyze(r.Context(), inputURL)
	if analyzeErr != nil {
		statusCode := analyzeErr.StatusCode
		if statusCode < 100 {
			statusCode = http.StatusBadRequest
		}
		writeJSON(w, statusCode, APIAnalyzeResponse{Result: result, Error: analyzeErr})
		return
	}

	writeJSON(w, http.StatusOK, APIAnalyzeResponse{Result: result})
}

// render executes the page template with common content-type handling.
func (h *WebAnalysisHandler) render(w http.ResponseWriter, data WebAnalysisViewData) {
	w.Header().Set(HeaderContentType, "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, "Unable to render page", http.StatusInternalServerError)
	}
}

// parseAPIURL reads the URL from JSON payloads when present, otherwise from form fields.
func parseAPIURL(r *http.Request) (string, *domain.PageAnalysisError) {
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get(HeaderContentType)))
	if strings.HasPrefix(contentType, "application/json") {
		var req APIAnalyzeRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			return "", domain.NewPageAnalysisError(http.StatusBadRequest, "Invalid JSON payload. Expected: {\"url\":\"https://example.com\"}")
		}
		return strings.TrimSpace(req.URL), nil
	}

	if err := r.ParseForm(); err != nil {
		return "", domain.NewPageAnalysisError(http.StatusBadRequest, "Invalid form payload")
	}

	return strings.TrimSpace(r.FormValue("url")), nil
}

// writeJSON serializes API responses and centralizes content-type/status handling.
func writeJSON(w http.ResponseWriter, statusCode int, payload APIAnalyzeResponse) {
	w.Header().Set(HeaderContentType, "application/json; charset=utf-8")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "{\"error\":{\"statusCode\":500,\"description\":\"Unable to encode response\"}}", http.StatusInternalServerError)
	}
}

// Analyze validates input, fetches the page, extracts metadata, and computes link accessibility stats.
func (a *HTTPAnalyzer) Analyze(parentCtx context.Context, rawURL string) (*domain.PageAnalysisResult, *domain.PageAnalysisError) {
	parsedURL, err := validateURL(rawURL)
	if err != nil {
		return nil, domain.NewPageAnalysisError(http.StatusBadRequest, err.Error())
	}

	ctx, cancel := context.WithTimeout(parentCtx, a.clientTimeout)
	defer cancel()

	redirects := 0
	client := &http.Client{
		Timeout: a.clientTimeout,
		// Limit redirect chains to avoid loops and preserve the redirect count for reporting.
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
	inaccessible, checked, skipped := a.countInaccessibleLinks(parentCtx, links)

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

// validateURL enforces supported schemes and basic host sanity before issuing outbound requests.
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

// mapRequestError converts low-level networking errors into user-facing analysis errors.
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

// analyzeHTML extracts headings, links, login-form signal, doctype/version, and page title.
func analyzeHTML(body []byte) ([]domain.HeadingCount, []discoveredLink, bool, string, string) {
	htmlBody := string(body)
	lowerBody := strings.ToLower(htmlBody)

	headingCounts := make([]domain.HeadingCount, 0, 6)
	for level := 1; level <= 6; level++ {
		// Count opening heading tags per level regardless of attributes/casing.
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

// countInternalLinks returns how many normalized links target the same hostname.
func countInternalLinks(links []discoveredLink) int {
	count := 0
	for _, link := range links {
		if link.Internal {
			count++
		}
	}
	return count
}

// countExternalLinks returns how many normalized links target a different hostname.
func countExternalLinks(links []discoveredLink) int {
	count := 0
	for _, link := range links {
		if !link.Internal {
			count++
		}
	}
	return count
}

// normalizeLinks resolves relative href values and removes unsupported/invalid targets.
func normalizeLinks(baseURL *url.URL, links []discoveredLink) []discoveredLink {
	normalized := make([]discoveredLink, 0, len(links))
	for _, link := range links {
		href := strings.TrimSpace(link.URL)
		if href == "" || strings.HasPrefix(href, "#") {
			continue
		}
		lowerHref := strings.ToLower(href)
		// Ignore non-http resources because they are not web-page accessibility targets.
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

// countInaccessibleLinks checks unique links concurrently and reports inaccessible/checked/skipped totals.
func (a *HTTPAnalyzer) countInaccessibleLinks(parentCtx context.Context, links []discoveredLink) (int, int, int) {
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
	if len(unique) > a.maxCheckedLinks {
		// Keep checks bounded for predictable response time on pages with many links.
		checkedTargets = unique[:a.maxCheckedLinks]
		skipped = len(unique) - a.maxCheckedLinks
	}

	ctx, cancel := context.WithTimeout(parentCtx, a.clientTimeout)
	defer cancel()

	const workers = 8
	type result struct{ inaccessible bool }

	jobs := make(chan string)
	results := make(chan result)
	var wg sync.WaitGroup

	// Fixed-size worker pool avoids spawning one goroutine per link on large pages.
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range jobs {
				results <- result{inaccessible: a.isLinkInaccessible(ctx, target)}
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

// isLinkInaccessible probes a link via HEAD first, then GET as fallback for servers that reject HEAD.
func (a *HTTPAnalyzer) isLinkInaccessible(parentCtx context.Context, target string) bool {
	ctx, cancel := context.WithTimeout(parentCtx, a.linkTimeout)
	defer cancel()

	client := &http.Client{Timeout: a.linkTimeout}
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
		// Retry with GET only for status codes that commonly indicate unsupported HEAD semantics.
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
