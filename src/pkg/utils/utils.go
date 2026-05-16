package utils

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

// APIPath builds a GitHub API URL for a given owner/repo and endpoint.
func APIPath(owner, repo, endpoint string) string {
	return fmt.Sprintf("%s/repos/%s/%s/%s", GithubAPI, owner, repo, endpoint)
}

// CheckStatus returns an error if the HTTP response status is not 2xx.
func CheckStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("API error: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
}

// ParseGitHubURL validates and extracts owner/repo from a GitHub URL
// Combines URL validation and parsing into a single function
func ParseGitHubURL(rawURL string) (owner, repo string, err error) {
	// Validate URL scheme and host
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL format: %w", err)
	}

	// Only allow https scheme
	if parsed.Scheme != "https" {
		return "", "", fmt.Errorf("only https:// scheme is allowed, got: %s", parsed.Scheme)
	}

	// Only allow github.com host
	if parsed.Host != "github.com" {
		return "", "", fmt.Errorf("only github.com host is allowed, got: %s", parsed.Host)
	}

	// Extract owner/repo from parsed.Path (e.g., "/owner/repo" or "/owner/repo.git")
	path := strings.TrimPrefix(parsed.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid GitHub URL path: %s", parsed.Path)
	}

	return parts[0], parts[1], nil
}

// ResolveRepo validates the URL and returns owner/repo, or a descriptive error.
func ResolveRepo(rawURL string) (owner, repo string, err error) {
	if rawURL == "" {
		return "", "", fmt.Errorf("missing required parameter: url")
	}
	return ParseGitHubURL(rawURL)
}

// RepoQualifierPattern is used to extract repo: qualifier from search queries
var RepoQualifierPattern = regexp.MustCompile(`repo:([^\s]+)`)

// branchPattern validates branch names (alphanumeric, /, _, -, .)
var branchPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/_.-]*$`)

// ValidateBranch validates a Git branch name
func ValidateBranch(branch string) error {
	if branch == "" || branch == "HEAD" {
		return nil
	}
	if !branchPattern.MatchString(branch) {
		return fmt.Errorf("invalid branch name: %s", branch)
	}
	return nil
}

// ParseGitHubSearchURL extracts owner/repo and query from GitHub search URLs
// Returns:
// - owner, repo, extractedQuery, nil for search URLs with repo: qualifier
// - "", "", query, nil for non-search URLs (caller should use resolveRepo)
// - "", "", "", err for search URLs without repo: qualifier
func ParseGitHubSearchURL(rawURL string) (string, string, string, error) {
	if rawURL == "" {
		return "", "", "", nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URL format: %w", err)
	}

	// Check if it's a GitHub search URL
	if parsed.Host != "github.com" || !strings.HasPrefix(parsed.Path, "/search") {
		return "", "", "", nil // Not a search URL
	}

	// Validate search type (must be code search, not issues/prs)
	queryParams := parsed.Query()
	searchType := queryParams.Get("type")
	if searchType != "" && searchType != "code" {
		return "", "", "", fmt.Errorf("only code search is supported, got type=%s", searchType)
	}

	q := queryParams.Get("q")
	if q == "" {
		return "", "", "", nil // No query in search URL
	}

	// Extract repo: qualifier using regexp
	matches := RepoQualifierPattern.FindStringSubmatch(q)
	if len(matches) >= 2 {
		repoValue := matches[1]
		// Validate owner/repo format
		if !OwnerRepoPattern.MatchString(repoValue) {
			return "", "", "", fmt.Errorf("invalid repo: qualifier format: %s", repoValue)
		}
		parts := strings.Split(repoValue, "/")
		if len(parts) == 2 {
			// Remove repo: qualifier from query for potential use
			extractedQuery := strings.TrimSpace(RepoQualifierPattern.ReplaceAllString(q, ""))
			if extractedQuery == "" {
				return "", "", "", fmt.Errorf("search URL has repo: qualifier but no search query")
			}
			return parts[0], parts[1], extractedQuery, nil
		}
	}

	// Search URL without repo: qualifier
	return "", "", "", fmt.Errorf("search URL must contain repo: qualifier for repository-specific search")
}

// TruncateResult holds truncation result with metadata
type TruncateResult struct {
	Content        string
	IsTruncated    bool
	OriginalLength int
	TruncatedAt    int
}

// Truncate truncates content if it exceeds limit, returns metadata
func Truncate(content string, limit int) TruncateResult {
	if len(content) <= limit {
		return TruncateResult{
			Content:        content,
			IsTruncated:    false,
			OriginalLength: len(content),
		}
	}
	return TruncateResult{
		Content:        content[:limit] + "... [TRUNCATED]",
		IsTruncated:    true,
		OriginalLength: len(content),
		TruncatedAt:    limit,
	}
}

// BuildTruncatedResponse builds a response map with truncated content
func BuildTruncatedResponse(base map[string]interface{}, content string, limit int, contentField string) map[string]interface{} {
	result := Truncate(content, limit)
	base[contentField] = result.Content
	if result.IsTruncated {
		base["is_truncated"] = true
		base["original_length"] = result.OriginalLength
		base["truncated_at"] = result.TruncatedAt
	}
	return base
}

// Debugf prints debug messages using slog when GIT_MCP_DEBUG=1
func Debugf(format string, args ...any) {
	if os.Getenv("GIT_MCP_DEBUG") != "1" {
		return
	}
	slog.Debug(fmt.Sprintf(format, args...))
}

// SemverDesc sorts version strings in descending SemVer order.
func SemverDesc(tags []string) {
	sort.Slice(tags, func(i, j int) bool {
		return CompareSemverDesc(tags[i], tags[j])
	})
}

// CompareSemverDesc compares two version strings for descending SemVer sort.
func CompareSemverDesc(a, b string) bool {
	va, vb := NormalizeSemver(a), NormalizeSemver(b)
	aValid, bValid := semver.IsValid(va), semver.IsValid(vb)
	switch {
	case aValid && bValid:
		return semver.Compare(va, vb) > 0
	case aValid:
		return true
	case bValid:
		return false
	default:
		return a > b
	}
}

// NormalizeSemver ensures version string has 'v' prefix
func NormalizeSemver(v string) string {
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// ReadResponseBody reads and returns the response body, limited to 1KB for error details
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(resp.Body, 1024))
}
