package domain

import "net/http"

type PageAnalysisError struct {
	StatusCode  int
	Description string
}

type HeadingCount struct {
	Level int
	Count int
}

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

func NewPageAnalysisError(statusCode int, description string) *PageAnalysisError {
	return &PageAnalysisError{StatusCode: statusCode, Description: description}
}

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
