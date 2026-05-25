package utils

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClassifyRateLimit_None(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		remaining   string
		retryAfter  string
	}{
		{"200 OK", 200, "5000", ""},
		{"200 OK no headers", 200, "", ""},
		{"404 Not Found", 404, "", ""},
		{"500 Internal", 500, "", ""},
		{"403 with remaining >0", 403, "10", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyRateLimit(tc.statusCode, tc.remaining, tc.retryAfter)
			if got != RateLimitNone {
				t.Errorf("ClassifyRateLimit(%d, %q, %q) = %v, want RateLimitNone",
					tc.statusCode, tc.remaining, tc.retryAfter, got)
			}
		})
	}
}

func TestClassifyRateLimit_Primary(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		remaining   string
		retryAfter  string
	}{
		{"429 with remaining=0", 429, "0", ""},
		{"403 with remaining=0", 403, "0", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyRateLimit(tc.statusCode, tc.remaining, tc.retryAfter)
			if got != RateLimitPrimary {
				t.Errorf("ClassifyRateLimit(%d, %q, %q) = %v, want RateLimitPrimary",
					tc.statusCode, tc.remaining, tc.retryAfter, got)
			}
		})
	}
}

func TestClassifyRateLimit_Secondary(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		remaining   string
		retryAfter  string
	}{
		{"429 with retry-after", 429, "5000", "60"},
		{"403 with retry-after", 403, "10", "30"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyRateLimit(tc.statusCode, tc.remaining, tc.retryAfter)
			if got != RateLimitSecondary {
				t.Errorf("ClassifyRateLimit(%d, %q, %q) = %v, want RateLimitSecondary",
					tc.statusCode, tc.remaining, tc.retryAfter, got)
			}
		})
	}
}

func TestClassifyRateLimitFull_FromHeadersPrimary(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{},
	}
	resp.Header.Set("X-RateLimit-Remaining", "0")
	if got := ClassifyRateLimitFull(resp); got != RateLimitPrimary {
		t.Errorf("ClassifyRateLimitFull = %v, want RateLimitPrimary", got)
	}
}

func TestClassifyRateLimitFull_FromHeadersSecondary(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
	}
	resp.Header.Set("Retry-After", "30")
	if got := ClassifyRateLimitFull(resp); got != RateLimitSecondary {
		t.Errorf("ClassifyRateLimitFull = %v, want RateLimitSecondary", got)
	}
}

func TestClassifyRateLimitFull_FromBodyPrimary(t *testing.T) {
	body := `{"message":"API rate limit exceeded","documentation_url":"https://docs.github.com/rest/overview#rate-limiting"}`
	resp := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	if got := ClassifyRateLimitFull(resp); got != RateLimitPrimary {
		t.Errorf("ClassifyRateLimitFull = %v, want RateLimitPrimary", got)
	}
}

func TestClassifyRateLimitFull_FromBodySecondary(t *testing.T) {
	body := `{"message":"You have secondary rate limited","documentation_url":"https://docs.github.com/rest/overview#secondary-rate-limits"}`
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	if got := ClassifyRateLimitFull(resp); got != RateLimitSecondary {
		t.Errorf("ClassifyRateLimitFull = %v, want RateLimitSecondary", got)
	}
}

func TestClassifyRateLimitFull_NoRateLimit(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
	}
	resp.Header.Set("X-RateLimit-Remaining", "5000")
	if got := ClassifyRateLimitFull(resp); got != RateLimitNone {
		t.Errorf("ClassifyRateLimitFull = %v, want RateLimitNone", got)
	}
}

func TestClassifyRateLimitFull_NonLimitStatusCode(t *testing.T) {
	body := `{"message":"Not Found","documentation_url":"https://docs.github.com/rest"}`
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	if got := ClassifyRateLimitFull(resp); got != RateLimitNone {
		t.Errorf("ClassifyRateLimitFull = %v, want RateLimitNone", got)
	}
}

func TestClassifyRateLimitBody_Nil(t *testing.T) {
	if got := ClassifyRateLimitBody(nil); got != RateLimitNone {
		t.Errorf("ClassifyRateLimitBody(nil) = %v, want RateLimitNone", got)
	}
}

func TestClassifyRateLimitBody_SecondaryByURL(t *testing.T) {
	signal := &RateLimitSignal{
		Message:          "You have been temporarily blocked",
		DocumentationURL: "https://docs.github.com/rest/overview#secondary-rate-limits",
	}
	if got := ClassifyRateLimitBody(signal); got != RateLimitSecondary {
		t.Errorf("ClassifyRateLimitBody = %v, want RateLimitSecondary", got)
	}
}

func TestClassifyRateLimitBody_SecondaryByAbuseURL(t *testing.T) {
	signal := &RateLimitSignal{
		Message:          "You have been abused",
		DocumentationURL: "https://docs.github.com/rest/overview#abuse-rate-limits",
	}
	if got := ClassifyRateLimitBody(signal); got != RateLimitSecondary {
		t.Errorf("ClassifyRateLimitBody = %v, want RateLimitSecondary", got)
	}
}

func TestClassifyRateLimitBody_SecondaryByMessage(t *testing.T) {
	signal := &RateLimitSignal{
		Message:          "secondary rate limit hit",
		DocumentationURL: "",
	}
	if got := ClassifyRateLimitBody(signal); got != RateLimitSecondary {
		t.Errorf("ClassifyRateLimitBody = %v, want RateLimitSecondary", got)
	}
}

func TestClassifyRateLimitBody_PrimaryByMessage(t *testing.T) {
	signal := &RateLimitSignal{
		Message:          "API rate limit exceeded",
		DocumentationURL: "https://docs.github.com/rest",
	}
	if got := ClassifyRateLimitBody(signal); got != RateLimitPrimary {
		t.Errorf("ClassifyRateLimitBody = %v, want RateLimitPrimary", got)
	}
}

func TestClassifyRateLimitBody_None(t *testing.T) {
	signal := &RateLimitSignal{
		Message:          "Not Found",
		DocumentationURL: "https://docs.github.com/rest",
	}
	if got := ClassifyRateLimitBody(signal); got != RateLimitNone {
		t.Errorf("ClassifyRateLimitBody = %v, want RateLimitNone", got)
	}
}
