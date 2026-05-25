package utils

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

// APIPath builds a GitHub API URL for a given owner/repo and endpoint.
func APIPath(owner, repo, endpoint string) string {
	return fmt.Sprintf("%s/repos/%s/%s/%s", GithubAPI, owner, repo, endpoint)
}

// SearchCodeURL builds a GitHub code search API URL with query and limit.
func SearchCodeURL(query string, limit int) string {
	params := url.Values{}
	params.Add("q", query)
	params.Add("per_page", fmt.Sprintf("%d", limit))
	return fmt.Sprintf("%s/search/code?%s", GithubAPI, params.Encode())
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
	path = strings.TrimSuffix(path, "/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid GitHub URL path: %s", parsed.Path)
	}

	owner, repo = parts[0], parts[1]

	if !OwnerRepoPattern.MatchString(owner + "/" + repo) {
		return "", "", fmt.Errorf("invalid GitHub URL: owner/repo format invalid: %s/%s", owner, repo)
	}

	return owner, repo, nil
}

// ResolveRepo validates the URL and returns owner/repo, or a descriptive error.
func ResolveRepo(rawURL string) (owner, repo string, err error) {
	if rawURL == "" {
		return "", "", fmt.Errorf("missing required parameter: url")
	}
	return ParseGitHubURL(rawURL)
}

// RepoQualifierPattern is used to extract repo: qualifier from search queries
var RepoQualifierPattern = regexp.MustCompile(`\brepo:([^\s]+)`)

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

// isGitHubSearchURL checks whether a parsed URL points to a GitHub search page.
func isGitHubSearchURL(parsed *url.URL) bool {
	return parsed.Host == "github.com" && strings.HasPrefix(parsed.Path, "/search")
}

// extractRepoQualifier parses the repo: qualifier from a GitHub search query string.
func extractRepoQualifier(q string) (owner, repo, query string, err error) {
	if q == "" {
		return "", "", "", nil
	}

	matches := RepoQualifierPattern.FindStringSubmatch(q)
	if len(matches) < 2 {
		return "", "", "", fmt.Errorf("search URL must contain repo: qualifier for repository-specific search")
	}

	repoValue := matches[1]
	if !OwnerRepoPattern.MatchString(repoValue) {
		return "", "", "", fmt.Errorf("invalid repo: qualifier format: %s", repoValue)
	}

	parts := strings.SplitN(repoValue, "/", 2)

	extractedQuery := strings.TrimSpace(RepoQualifierPattern.ReplaceAllString(q, ""))
	if extractedQuery == "" {
		return "", "", "", fmt.Errorf("search URL has repo: qualifier but no search query")
	}

	return parts[0], parts[1], extractedQuery, nil
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

	if !isGitHubSearchURL(parsed) {
		return "", "", "", nil
	}

	queryParams := parsed.Query()
	searchType := queryParams.Get("type")
	if searchType != "" && searchType != "code" {
		return "", "", "", fmt.Errorf("only code search is supported, got type=%s", searchType)
	}

	return extractRepoQualifier(queryParams.Get("q"))
}

// TruncateResult holds truncation result with metadata
type TruncateResult struct {
	Content        string
	IsTruncated    bool
	OriginalLength int64
	TruncatedAt    int
}

// Truncate truncates content if it exceeds limit, returns metadata
func Truncate(content string, limit int) TruncateResult {
	runes := []rune(content)
	if len(runes) <= limit {
		return TruncateResult{
			Content:        content,
			IsTruncated:    false,
			OriginalLength: int64(len(content)),
		}
	}
	return TruncateResult{
		Content:        string(runes[:limit]) + "... [TRUNCATED]",
		IsTruncated:    true,
		OriginalLength: int64(len(content)),
		TruncatedAt:    limit,
	}
}

// BuildTruncatedResponse builds a response map with truncated content
func BuildTruncatedResponse(
	base map[string]interface{},
	content string,
	limit int,
	contentField string,
) map[string]interface{} {
	result := Truncate(content, limit)
	base[contentField] = result.Content
	if result.IsTruncated {
		base["is_truncated"] = true
		base["original_length"] = result.OriginalLength
		base["truncated_at"] = result.TruncatedAt
	}
	return base
}

// Debugf prints debug messages using slog (controlled by slog level).
// Skips formatting entirely when debug logging is not enabled.
func Debugf(format string, args ...any) {
	if slog.Default().Enabled(context.Background(), slog.LevelDebug) {
		slog.Debug(fmt.Sprintf(format, args...))
	}
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
