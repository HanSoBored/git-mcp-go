package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// rateLimitHeaders holds parsed GitHub rate-limit response headers.
type rateLimitHeaders struct {
	RetryAfter string
	ResetAt    string
	Resource   string
	Remaining  string
}

// DiagnoseError reads the response body and rate-limit headers to produce
// a specific, actionable error message.
func DiagnoseError(resp *http.Response) error {
	code := resp.StatusCode
	if code >= 200 && code < 300 {
		// Caller still needs resp.Body — do NOT close it here.
		return nil
	}

	signal := ParseRateLimitResponse(resp)
	var body, msg string
	if signal != nil {
		body = signal.BodyText
		msg = signal.Message
	}
	h := extractRateLimitHeaders(resp)

	switch {
	case code == http.StatusUnauthorized:
		return diagnose401(msg)
	case code == http.StatusTooManyRequests:
		return diagnose429(msg, h)
	case code == http.StatusForbidden:
		return diagnose403(msg, body, h)
	default:
		return diagnoseDefault(code, msg, body)
	}
}

// extractRateLimitHeaders pulls all rate-limit-related headers from the response.
func extractRateLimitHeaders(resp *http.Response) rateLimitHeaders {
	return rateLimitHeaders{
		Remaining:  resp.Header.Get("X-RateLimit-Remaining"),
		ResetAt:    resp.Header.Get("X-RateLimit-Reset"),
		Resource:   resp.Header.Get("X-RateLimit-Resource"),
		RetryAfter: resp.Header.Get("Retry-After"),
	}
}

// RateLimitSignal holds parsed rate-limit response data.
type RateLimitSignal struct {
	Message          string
	DocumentationURL string
	BodyText         string
}

// ParseRateLimitResponse reads the response body and extracts rate-limit signals.
// Returns nil if body read fails or is empty.
// The response body is always rewrapped so downstream consumers are unaffected.
func ParseRateLimitResponse(resp *http.Response) *RateLimitSignal {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	// Always rewrap resp.Body to protect downstream consumers, even on error.
	resp.Body = io.NopCloser(bytes.NewBuffer(body))
	if err != nil || len(body) == 0 {
		return nil
	}

	var parsed struct {
		Message          string `json:"message"`
		DocumentationURL string `json:"documentation_url"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		slog.Debug("failed to parse GitHub error body as JSON",
			"error", err,
			"body_preview", string(body[:min(len(body), 200)]),
		)
	}

	return &RateLimitSignal{
		Message:          parsed.Message,
		DocumentationURL: parsed.DocumentationURL,
		BodyText:         string(body),
	}
}

// diagnose401 handles HTTP 401 Unauthorized by identifying bad credentials vs missing auth.
func diagnose401(msg string) error {
	if msg == "Bad credentials" {
		return fmt.Errorf("HTTP 401 - Bad credentials: GITHUB_TOKEN is invalid or expired")
	}
	return fmt.Errorf("HTTP 401 - Authentication required: set a valid GITHUB_TOKEN environment variable")
}

// classifyFromFields classifies the rate-limit type from status code, message, and headers.
func classifyFromFields(code int, msg string, h rateLimitHeaders) RateLimitType {
	rlType := ClassifyRateLimit(code, h.Remaining, h.RetryAfter)
	if rlType == RateLimitNone && msg != "" {
		rlType = ClassifyRateLimitBody(&RateLimitSignal{Message: msg})
	}
	return rlType
}

// diagnoseRateLimit builds a rate-limit error for both primary and secondary limits.
func diagnoseRateLimit(code int, h rateLimitHeaders, rlType RateLimitType) error {
	switch rlType {
	case RateLimitSecondary:
		return fmt.Errorf(
			"HTTP %d - Secondary rate limit exceeded%s%s. Reduce request concurrency and frequency",
			code, resourceInfo(h.Resource), retryAfterInfo(h.RetryAfter),
		)
	case RateLimitPrimary:
		return fmt.Errorf(
			"HTTP %d - Primary rate limit exceeded%s%s. Authenticate with GITHUB_TOKEN for 5,000 requests/hour",
			code, rateLimitResetInfo(h.ResetAt), resourceInfo(h.Resource),
		)
	default:
		return fmt.Errorf("HTTP %d - Rate limit exceeded%s%s",
			code, rateLimitResetInfo(h.ResetAt), retryAfterInfo(h.RetryAfter),
		)
	}
}

// diagnose429 handles HTTP 429 Too Many Requests by classifying the rate-limit type.
func diagnose429(msg string, h rateLimitHeaders) error {
	rlType := classifyFromFields(429, msg, h)
	return diagnoseRateLimit(429, h, rlType)
}

// diagnose403 handles HTTP 403 Forbidden, checking for rate limits and credential errors.
func diagnose403(msg, body string, h rateLimitHeaders) error {
	rlType := classifyFromFields(403, msg, h)
	if rlType == RateLimitSecondary || rlType == RateLimitPrimary {
		return diagnoseRateLimit(403, h, rlType)
	}
	switch {
	case msg == "Bad credentials" || strings.Contains(msg, "bad credentials") || strings.Contains(msg, "token"):
		return fmt.Errorf("HTTP 403 - Bad credentials: GITHUB_TOKEN is invalid or lacks repository permissions")
	case msg != "":
		return fmt.Errorf("HTTP 403 - Forbidden: %s", msg)
	case body != "":
		return fmt.Errorf("HTTP 403 - Forbidden: %s", shorten(body, 200))
	default:
		return fmt.Errorf("HTTP 403 - Forbidden: check your GITHUB_TOKEN and repository access")
	}
}

// diagnoseDefault handles non-rate-limit HTTP errors by extracting the message from body.
func diagnoseDefault(code int, msg, body string) error {
	switch {
	case msg != "":
		return fmt.Errorf("HTTP %d - %s", code, msg)
	case body != "":
		return fmt.Errorf("HTTP %d - %s", code, shorten(body, 200))
	default:
		return fmt.Errorf("API error: HTTP %d %s", code, http.StatusText(code))
	}
}

// resourceInfo formats the rate-limit resource information for error messages.
func resourceInfo(resource string) string {
	if resource == "" {
		return ""
	}
	return fmt.Sprintf(" [resource: %s]", resource)
}

// retryAfterInfo formats the Retry-After header value for error messages.
func retryAfterInfo(retryAfter string) string {
	if retryAfter == "" {
		return ""
	}
	return fmt.Sprintf(" - retry after %ss", retryAfter)
}

// rateLimitResetInfo formats the X-RateLimit-Reset timestamp for error messages.
func rateLimitResetInfo(resetAt string) string {
	if resetAt == "" {
		return ""
	}
	unixSec, err := strconv.ParseInt(resetAt, 10, 64)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(" - resets at %s", time.Unix(unixSec, 0).UTC().Format(time.RFC3339))
}

// shorten truncates a string to at most n runes, appending "..." if truncated.
func shorten(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
