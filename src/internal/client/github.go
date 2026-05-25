package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"git-mcp/pkg/utils"
	"git-mcp/pkg/version"
)

const (
	dnsTimeout           = 5 * time.Second
	tlsTimeout           = 10 * time.Second
	maxResponseBodySize  = 10 << 20 // 10 MB
	maxSecondaryAttempts = 5
	minRetryBackoff      = 1 * time.Second
)

// isPrivateIP checks if an IP address is private or internal.
// Includes loopback, RFC 1918 private, link-local, and RFC 6598 CGNAT ranges.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	// RFC 6598: Carrier-Grade NAT (CGNAT) range 100.64.0.0/10
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1]&0xC0 == 0x40 {
			return true
		}
	}
	return false
}

// secureDialer creates a net.Dialer with SSRF protection via Control callback.
// The Control callback is called AFTER DNS resolution but BEFORE connect syscall,
// inspecting the exact IP being connected to (prevents TOCTOU race conditions).
func secureDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   tlsTimeout,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			ip := net.ParseIP(host)
			if ip != nil && isPrivateIP(ip) {
				return fmt.Errorf("connection to private/internal IP blocked: %s", ip)
			}
			return nil
		},
	}
}

// checkBlockedHostname verifies that the hostname in a URL is not in the
// blocked list (e.g., SSRF protection for metadata endpoints).
func checkBlockedHostname(host string) error {
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || strings.HasPrefix(lowerHost, "metadata.") {
		return fmt.Errorf("redirect to dangerous hostname blocked: %s", host)
	}
	for _, suffix := range []string{".localhost", ".local", ".internal"} {
		if strings.HasSuffix(lowerHost, suffix) {
			return fmt.Errorf("redirect to dangerous hostname blocked: %s", host)
		}
	}
	if strings.Contains(lowerHost, "nip.io") || strings.Contains(lowerHost, "sslip.io") {
		return fmt.Errorf("redirect to DNS-based SSRF bypass domain blocked: %s", host)
	}
	return nil
}

// GithubClient encapsulates GitHub API HTTP client with token rotation
type GithubClient struct {
	httpClient  *http.Client
	rotator     *TokenRotator
	maxAttempts int
}

// NewGithubClient creates a new GitHub API client with the given tokens
func NewGithubClient(tokens ...string) *GithubClient {
	rotator := NewTokenRotator(tokens)
	maxAttempts := rotator.Len()
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return &GithubClient{
		httpClient: &http.Client{
			Timeout: utils.GithubAPITimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if err := checkBlockedHostname(req.URL.Host); err != nil {
					slog.Warn("redirect blocked by SSRF protection",
						"host", req.URL.Host,
						"url", req.URL.String(),
						"redirect_count", len(via),
					)
					return err
				}
				// Limit redirect depth
				if len(via) >= 10 {
					return fmt.Errorf("stopped after %d redirects", len(via))
				}
				return nil
			},
			Transport: &http.Transport{
				DialContext:           secureDialer().DialContext,
				TLSHandshakeTimeout:   tlsTimeout,
				ResponseHeaderTimeout: utils.GithubAPITimeout,
				IdleConnTimeout:       90 * time.Second,
			},
		},
		rotator:     rotator,
		maxAttempts: maxAttempts,
	}
}

// Get executes a GET request to GitHub API with automatic token rotation on rate limits.
func (c *GithubClient) Get(ctx context.Context, apiPath, acceptHeader string) (*http.Response, error) {
	return c.getWithRetry(ctx, apiPath, acceptHeader)
}

// getWithRetry performs GET with bounded retries, token rotation on primary rate limits,
// and context-aware backoff on secondary rate limits.
// Primary rate limits are bounded by the configured maxAttempts (one per token).
// Secondary rate limits have a separate, independent retry budget.
func (c *GithubClient) getWithRetry(ctx context.Context, apiPath, acceptHeader string) (*http.Response, error) {
	primaryAttempts := 0
	secondaryAttempts := 0

	for {
		resp, err := c.doRequest(ctx, apiPath, acceptHeader)
		if err != nil {
			return nil, err
		}

		rlType := utils.ClassifyRateLimitFull(resp)
		if rlType == utils.RateLimitNone {
			return resp, nil
		}

		resp.Body.Close()

		switch rlType {
		case utils.RateLimitPrimary:
			primaryAttempts++
			if primaryAttempts >= c.maxAttempts {
				return nil, fmt.Errorf("request failed after %d attempt(s) due to rate limiting", c.maxAttempts)
			}
			if err := c.rotateToken(resp.StatusCode); err != nil {
				return nil, err
			}
		case utils.RateLimitSecondary:
			secondaryAttempts++
			if secondaryAttempts >= maxSecondaryAttempts {
				return nil, fmt.Errorf("request failed after %d secondary rate limit retries", maxSecondaryAttempts)
			}
			if err := c.handleSecondaryRateLimit(ctx, resp); err != nil {
				return nil, err
			}
		}
	}
}

// handleSecondaryRateLimit handles secondary rate limiting by parsing the Retry-After
// header and backing off with a context-aware sleep.
func (c *GithubClient) handleSecondaryRateLimit(ctx context.Context, resp *http.Response) error {
	backoff := parseRetryAfter(resp)
	slog.Warn("GitHub API secondary rate limit hit, backing off",
		"backoff", backoff,
		"max_retries", maxSecondaryAttempts,
	)

	timer := time.NewTimer(backoff)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// doRequest creates and executes an HTTP GET request with GitHub API headers.
func (c *GithubClient) doRequest(ctx context.Context, apiPath, acceptHeader string) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context done during retry: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setRequestHeaders(req, acceptHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github API request failed: %w", err)
	}

	return resp, nil
}

// setRequestHeaders sets standard GitHub API headers on the request.
func (c *GithubClient) setRequestHeaders(req *http.Request, acceptHeader string) {
	req.Header.Set("User-Agent", fmt.Sprintf("git-mcp-go/%s", version.Get()))

	token := c.rotator.Current()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if acceptHeader != "" {
		req.Header.Set("Accept", acceptHeader)
	}
}

// rotateToken rotates to the next available token on rate limit.
func (c *GithubClient) rotateToken(statusCode int) error {
	_, exhausted, ok := c.rotator.Rotate()
	if !ok {
		return fmt.Errorf("rate limited with no tokens configured. Set GITHUB_TOKEN env var")
	}
	if exhausted {
		return fmt.Errorf(
			"all %d GitHub tokens rate limited (HTTP %d). Set more GITHUB_TOKEN_N env vars or wait for reset",
			c.rotator.Len(), statusCode,
		)
	}
	return nil
}

// parseRetryAfter extracts the backoff duration from the response headers.
// Precedence:
//  1. Retry-After header (non-negative integer, floored to minRetryBackoff)
//  2. X-RateLimit-Reset header (if > 1s from now)
//  3. Default 60s
func parseRetryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			if secs == 0 {
				return minRetryBackoff
			}
			d := time.Duration(secs) * time.Second
			if d < minRetryBackoff {
				return minRetryBackoff
			}
			return d
		}
		if retryTime, err := time.Parse(time.RFC1123, v); err == nil {
			d := time.Until(retryTime)
			if d < minRetryBackoff {
				return minRetryBackoff
			}
			if d > 0 {
				return d
			}
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			d := time.Until(time.Unix(epoch, 0))
			if d > time.Second {
				return d
			}
		}
	}
	return 60 * time.Second
}

// DoRaw performs GET → DiagnoseError → decode JSON in a single call.
func (c *GithubClient) DoRaw(ctx context.Context, apiURL, accept string, result any) error {
	resp, err := c.Get(ctx, apiURL, accept)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := utils.DiagnoseError(resp); err != nil {
		return err
	}
	return json.NewDecoder(io.LimitReader(resp.Body, maxResponseBodySize)).Decode(result)
}

// DoAPI handles the common pattern: build API path → GET → diagnose → decode JSON
func (c *GithubClient) DoAPI(ctx context.Context, owner, repo, endpoint, accept string, result any) error {
	apiURL := utils.APIPath(owner, repo, endpoint)
	return c.DoRaw(ctx, apiURL, accept, result)
}

// DoAPIRaw handles the common pattern: build API path → GET → diagnose → read raw body
func (c *GithubClient) DoAPIRaw(ctx context.Context, owner, repo, endpoint, accept string) ([]byte, error) {
	apiURL := utils.APIPath(owner, repo, endpoint)
	resp, err := c.Get(ctx, apiURL, accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := utils.DiagnoseError(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
}
