package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ashwinsekaran/webpage-analysis-go/domain"
)

type mockAnalyzer struct {
	calledWith string
	result     *domain.PageAnalysisResult
	err        *domain.PageAnalysisError
}

func (m *mockAnalyzer) Analyze(_ context.Context, rawURL string) (*domain.PageAnalysisResult, *domain.PageAnalysisError) {
	m.calledWith = rawURL
	return m.result, m.err
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid https", input: "https://example.com", wantErr: false},
		{name: "valid http", input: "http://example.com/path", wantErr: false},
		{name: "empty", input: "", wantErr: true},
		{name: "junk", input: "hello world", wantErr: true},
		{name: "unsupported scheme", input: "ftp://example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateURL(tt.input)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for input %q", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for input %q: %v", tt.input, err)
			}
		})
	}
}

func TestAnalyzeHTML(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
  <head><title>Test Page</title></head>
  <body>
    <h1>Main</h1>
    <h2>Section</h2>
    <h2>Section 2</h2>
    <a href="/internal">Internal</a>
    <a href="https://external.example.com">External</a>
    <form action="/login" method="post">
      <input type="text" name="username" />
      <input type="password" name="password" />
    </form>
  </body>
</html>`

	headings, links, hasLogin, htmlVersion, title := analyzeHTML([]byte(html))

	if htmlVersion != "HTML5" {
		t.Fatalf("html version mismatch: got %q", htmlVersion)
	}
	if title != "Test Page" {
		t.Fatalf("title mismatch: got %q", title)
	}
	if !hasLogin {
		t.Fatal("expected login form to be detected")
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
	if headings[0].Count != 1 || headings[1].Count != 2 {
		t.Fatalf("unexpected heading counts: %+v", headings)
	}
}

func TestNormalizeLinks(t *testing.T) {
	base, err := url.Parse("https://example.com/path")
	if err != nil {
		t.Fatalf("parse base url: %v", err)
	}

	links := []discoveredLink{
		{URL: "/internal"},
		{URL: "https://google.com"},
		{URL: "#fragment"},
		{URL: "javascript:void(0)"},
	}

	normalized := normalizeLinks(base, links)
	if len(normalized) != 2 {
		t.Fatalf("expected 2 normalized links, got %d", len(normalized))
	}
	if !normalized[0].Internal {
		t.Fatal("expected first link to be internal")
	}
	if normalized[1].Internal {
		t.Fatal("expected second link to be external")
	}
}

func TestWebAnalysisHandlerPostUsesAnalyzer(t *testing.T) {
	templateDir := t.TempDir()
	writeTemplate(t, filepath.Join(templateDir, "layout.gohtml"), `{{define "layout"}}{{template "header" .}}{{template "content" .}}{{template "footer" .}}{{end}}`)
	writeTemplate(t, filepath.Join(templateDir, "header.gohtml"), `{{define "header"}}HEADER{{end}}`)
	writeTemplate(t, filepath.Join(templateDir, "content.gohtml"), `{{define "content"}}{{.InputURL}}|{{if .Result}}{{.Result.StatusText}}{{end}}{{if .Error}}|{{.Error.Description}}{{end}}{{end}}`)
	writeTemplate(t, filepath.Join(templateDir, "footer.gohtml"), `{{define "footer"}}FOOTER{{end}}`)

	mock := &mockAnalyzer{
		result: &domain.PageAnalysisResult{StatusText: "200 OK"},
	}

	h, err := NewWebAnalysisHandler(templateDir, mock)
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	form := strings.NewReader("url=https%3A%2F%2Fexample.com")
	req := httptest.NewRequest(http.MethodPost, "/", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	h.Post(rr, req, nil)

	if mock.calledWith != "https://example.com" {
		t.Fatalf("analyzer called with %q", mock.calledWith)
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", rr.Code)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "https://example.com|200 OK") {
		t.Fatalf("unexpected response body: %q", body)
	}
}

func TestWebAnalysisHandlerAnalyzeAPISuccess(t *testing.T) {
	templateDir := t.TempDir()
	writeTemplate(t, filepath.Join(templateDir, "layout.gohtml"), `{{define "layout"}}{{end}}`)
	writeTemplate(t, filepath.Join(templateDir, "header.gohtml"), `{{define "header"}}{{end}}`)
	writeTemplate(t, filepath.Join(templateDir, "content.gohtml"), `{{define "content"}}{{end}}`)
	writeTemplate(t, filepath.Join(templateDir, "footer.gohtml"), `{{define "footer"}}{{end}}`)

	mock := &mockAnalyzer{
		result: &domain.PageAnalysisResult{
			StatusCode: 200,
			StatusText: "200 OK",
		},
	}

	h, err := NewWebAnalysisHandler(templateDir, mock)
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(`{"url":"https://example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.AnalyzeAPI(rr, req, nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", rr.Code)
	}
	if mock.calledWith != "https://example.com" {
		t.Fatalf("analyzer called with %q", mock.calledWith)
	}

	var payload APIAnalyzeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
	if payload.Result == nil || payload.Result.StatusText != "200 OK" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestWebAnalysisHandlerAnalyzeAPIBadJSON(t *testing.T) {
	templateDir := t.TempDir()
	writeTemplate(t, filepath.Join(templateDir, "layout.gohtml"), `{{define "layout"}}{{end}}`)
	writeTemplate(t, filepath.Join(templateDir, "header.gohtml"), `{{define "header"}}{{end}}`)
	writeTemplate(t, filepath.Join(templateDir, "content.gohtml"), `{{define "content"}}{{end}}`)
	writeTemplate(t, filepath.Join(templateDir, "footer.gohtml"), `{{define "footer"}}{{end}}`)

	h, err := NewWebAnalysisHandler(templateDir, &mockAnalyzer{})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(`{"url":`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.AnalyzeAPI(rr, req, nil)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", rr.Code)
	}

	var payload APIAnalyzeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
	if payload.Error == nil {
		t.Fatalf("expected error payload, got %+v", payload)
	}
}

func writeTemplate(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write template %s: %v", path, err)
	}
}
