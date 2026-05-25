package utils

import (
	"net/http"
	"net/url"
	"strings"
)

// RateLimitType classifies a GitHub API rate-limit response.
type RateLimitType int

const (
	RateLimitNone      RateLimitType = iota // No rate limit detected
	RateLimitPrimary                        // Primary (user-based) rate limit
	RateLimitSecondary                      // Secondary (abuse-detection) rate limit
)

func isRateLimitStatusCode(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusForbidden
}

// ClassifyRateLimit determines rate-limit type from HTTP status code and headers.
// Returns RateLimitPrimary, RateLimitSecondary, or RateLimitNone.
func ClassifyRateLimit(statusCode int, remaining, retryAfter string) RateLimitType {
	if !isRateLimitStatusCode(statusCode) {
		return RateLimitNone
	}
	if retryAfter != "" {
		return RateLimitSecondary
	}
	if remaining == "0" {
		return RateLimitPrimary
	}
	return RateLimitNone
}

// isSecondaryRateLimitURL checks if the documentation URL fragment indicates
// a secondary rate limit by parsing the URL with net/url for robust matching.
func isSecondaryRateLimitURL(docURL string) bool {
	if docURL == "" {
		return false
	}
	parsed, err := url.Parse(docURL)
	if err != nil {
		return false
	}
	return parsed.Fragment == "secondary-rate-limits" || parsed.Fragment == "abuse-rate-limits"
}

// ClassifyRateLimitBody determines rate-limit type from a parsed response body signal.
// Returns RateLimitPrimary, RateLimitSecondary, or RateLimitNone.
func ClassifyRateLimitBody(signal *RateLimitSignal) RateLimitType {
	if signal == nil {
		return RateLimitNone
	}
	msg := strings.ToLower(signal.Message)
	switch {
	case isSecondaryRateLimitURL(signal.DocumentationURL):
		return RateLimitSecondary
	case strings.Contains(msg, "secondary"):
		return RateLimitSecondary
	case strings.Contains(msg, "rate limit"):
		return RateLimitPrimary
	}
	return RateLimitNone
}

// ClassifyRateLimitFull inspects an HTTP response for rate-limit signals.
// It checks headers first (fast path). If headers are inconclusive and the
// status code suggests a rate limit (429, 403), it reads the body once to
// classify the type. The body is always rewrapped so downstream consumers
// (e.g. DiagnoseError) are unaffected.
func ClassifyRateLimitFull(resp *http.Response) RateLimitType {
	rlType := ClassifyRateLimit(
		resp.StatusCode,
		resp.Header.Get("X-RateLimit-Remaining"),
		resp.Header.Get("Retry-After"),
	)
	if rlType != RateLimitNone {
		return rlType
	}

	// Headers didn't indicate rate limit, but body might for 429/403
	if !isRateLimitStatusCode(resp.StatusCode) {
		return RateLimitNone
	}

	signal := ParseRateLimitResponse(resp)
	if signal == nil {
		return RateLimitNone
	}

	return ClassifyRateLimitBody(signal)
}
