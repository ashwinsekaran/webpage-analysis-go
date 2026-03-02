package domain

import "testing"

func TestApplyStatusPresentation(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		tagClass   string
		circle     string
	}{
		{name: "200 success", statusCode: 200, tagClass: "is-success", circle: "has-text-success"},
		{name: "500 error", statusCode: 500, tagClass: "is-danger", circle: "has-text-danger"},
		{name: "302 warning", statusCode: 302, tagClass: "is-warning", circle: "has-text-warning"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &PageAnalysisResult{StatusCode: tt.statusCode}
			result.ApplyStatusPresentation()

			if result.StatusTagClass != tt.tagClass {
				t.Fatalf("StatusTagClass mismatch: got %q want %q", result.StatusTagClass, tt.tagClass)
			}
			if result.StatusCircleClass != tt.circle {
				t.Fatalf("StatusCircleClass mismatch: got %q want %q", result.StatusCircleClass, tt.circle)
			}
		})
	}
}

func TestNewPageAnalysisError(t *testing.T) {
	err := NewPageAnalysisError(400, "bad request")
	if err.StatusCode != 400 || err.Description != "bad request" {
		t.Fatalf("unexpected error struct: %+v", err)
	}
}
