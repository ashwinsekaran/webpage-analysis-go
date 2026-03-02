package domain

import "net/http"

// PageAnalysisError is a user-facing error payload returned by page analysis flows.
type PageAnalysisError struct {
	// StatusCode mirrors an HTTP status-style code suitable for UI/API responses.
	StatusCode  int
	// Description is a readable explanation of the failure reason.
	Description string
}

// HeadingCount stores the number of heading tags found for a given level (h1-h6).
type HeadingCount struct {
	Level int
	Count int
}

// PageAnalysisResult holds all extracted metadata and computed statistics for a page.
type PageAnalysisResult struct {
	RequestedURL       string
	FinalURL           string
	StatusCode         int
	StatusText         string
	StatusTagClass     string
	StatusCircleClass  string
	HTMLVersion        string
	PageTitle          string
	HeadingCounts      []HeadingCount
	InternalLinks      int
	ExternalLinks      int
	TotalLinks         int
	InaccessibleLinks  int
	CheckedLinks       int
	SkippedLinkChecks  int
	HasLoginForm       bool
	ResponseTime       string
	ContentType        string
	ContentLength      int64
	ServerHeader       string
	RedirectCount      int
	AnalysisFinishedAt string
}

// NewPageAnalysisError builds a PageAnalysisError with the provided status and description.
func NewPageAnalysisError(statusCode int, description string) *PageAnalysisError {
	return &PageAnalysisError{StatusCode: statusCode, Description: description}
}

// ApplyStatusPresentation maps the HTTP status code to UI style classes.
func (r *PageAnalysisResult) ApplyStatusPresentation() {
	if r == nil {
		return
	}
	if r.StatusCode == http.StatusOK {
		r.StatusTagClass = "is-success"
		r.StatusCircleClass = "has-text-success"
		return
	}
	if r.StatusCode >= http.StatusBadRequest {
		r.StatusTagClass = "is-danger"
		r.StatusCircleClass = "has-text-danger"
		return
	}
	r.StatusTagClass = "is-warning"
	r.StatusCircleClass = "has-text-warning"
}
