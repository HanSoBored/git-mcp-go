package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"git-mcp/pkg/utils"
	"git-mcp/pkg/version"
)

const dnsTimeout = 5 * time.Second

// isPrivateIP checks if an IP address is private or internal
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	return false
}

// secureDialer creates a net.Dialer with SSRF protection via Control callback.
// The Control callback is called AFTER DNS resolution but BEFORE connect syscall,
// inspecting the exact IP being connected to (prevents TOCTOU race conditions).
func secureDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   10 * time.Second,
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

// singletonResolver is a package-level singleton resolver for DNS lookups
var singletonResolver = &net.Resolver{
	PreferGo: true,
	Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		d := net.Dialer{Timeout: dnsTimeout}
		return d.DialContext(ctx, network, address)
	},
}

// secureResolver creates a net.Resolver with timeout protection
func secureResolver() *net.Resolver {
	return singletonResolver
}

// checkRedirect prevents redirects to private/internal IPs (SSRF protection)
func checkRedirect(req *http.Request, via []*http.Request) error {
	host, _, err := net.SplitHostPort(req.URL.Host)
	if err != nil {
		host = req.URL.Host
	}

	// Block known dangerous hostnames
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || strings.HasPrefix(lowerHost, "metadata.") || strings.HasSuffix(lowerHost, ".internal") {
		return fmt.Errorf("redirect to dangerous hostname blocked: %s", host)
	}

	// Block dangerous suffixes
	dangerousSuffixes := []string{".local", ".localhost", ".internal"}
	for _, suffix := range dangerousSuffixes {
		if strings.HasSuffix(lowerHost, suffix) {
			return fmt.Errorf("redirect to dangerous hostname blocked: %s", host)
		}
	}

	// Catch DNS-based SSRF bypasses (nip.io, sslip.io)
	if strings.Contains(lowerHost, "nip.io") || strings.Contains(lowerHost, "sslip.io") {
		return fmt.Errorf("redirect to DNS-based SSRF bypass domain blocked: %s", host)
	}

	// Resolve hostname to IPs with timeout and validate each
	// Note: This is a defense-in-depth check; the primary protection is the
	// secureDialer Control callback which runs after DNS resolution
	resolveCtx, cancel := context.WithTimeout(req.Context(), dnsTimeout)
	defer cancel()
	
	ips, err := secureResolver().LookupIP(resolveCtx, "ip", host)
	if err == nil {
		for _, ip := range ips {
			if isPrivateIP(ip) {
				return fmt.Errorf("redirect to private/internal IP blocked: %s resolves to %s", host, ip.String())
			}
		}
	}

	// Fallback: check if host itself is an IP
	ip := net.ParseIP(host)
	if ip != nil && isPrivateIP(ip) {
		return fmt.Errorf("redirect to private/internal IP blocked: %s", host)
	}

	return nil
}

// GithubClient encapsulates GitHub API HTTP client with authentication
type GithubClient struct {
	httpClient *http.Client
	token      string
}

// NewGithubClient creates a new GitHub API client with the given token
func NewGithubClient(token string) *GithubClient {
	return &GithubClient{
		httpClient: &http.Client{
			Timeout:       utils.GithubAPITimeout,
			CheckRedirect: checkRedirect,
			Transport: &http.Transport{
				DialContext:           secureDialer().DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: utils.GithubAPITimeout,
				IdleConnTimeout:       90 * time.Second,
			},
		},
		token: token,
	}
}

// Get executes a GET request to GitHub API with proper headers
func (c *GithubClient) Get(ctx context.Context, apiPath string, acceptHeader string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", fmt.Sprintf("git-mcp-go/%s", version.Get()))

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.token))
	}

	if acceptHeader != "" {
		req.Header.Set("Accept", acceptHeader)
	}

	return c.httpClient.Do(req)
}

// DecodeJSON decodes JSON response body into the provided value
func (c *GithubClient) DecodeJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// DoAPI handles the common pattern: build API path → GET → check status → decode JSON
func (c *GithubClient) DoAPI(ctx context.Context, owner, repo, endpoint, accept string, result any) error {
	apiUrl := utils.APIPath(owner, repo, endpoint)
	resp, err := c.Get(ctx, apiUrl, accept)
	if err != nil {
		return err
	}
	if err := utils.CheckStatus(resp); err != nil {
		resp.Body.Close()
		return err
	}
	return c.DecodeJSON(resp, result)
}

// DoAPIRaw handles the common pattern: build API path → GET → check status → read raw body
func (c *GithubClient) DoAPIRaw(ctx context.Context, owner, repo, endpoint, accept string) ([]byte, error) {
	apiUrl := utils.APIPath(owner, repo, endpoint)
	resp, err := c.Get(ctx, apiUrl, accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}
