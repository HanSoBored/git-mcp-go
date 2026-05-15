package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"
)

// ============================================================================
// Constants - Magic numbers replaced with named constants
// ============================================================================
const (
	maxScannerBufferSize  = 10 * 1024 * 1024 // Maximum scanner buffer size (10MB)
	maxReadmeLength       = 20000            // Maximum README content length before truncation
	maxFileContentLength  = 30000            // Maximum file content length before truncation
	maxFileTreeEntries    = 1000             // Maximum file tree entries before truncation
	maxChangelogEntries   = 200              // Maximum changelog entries before truncation
	githubAPITimeout      = 30 * time.Second // Timeout for GitHub API requests
	githubAPI             = "https://api.github.com"
	maxLanguageFilterLen  = 20
	maxFilenameFilterLen  = 100
	maxAuthorFilterLen    = 39 // GitHub username max length
	maxDateFilterLen      = 30 // e.g. "2024-01-01..2024-12-31"
	maxSearchQueryLen     = 512
	maxListLimit          = 100
	defaultListLimit      = 30
	maxLabelsFilterLen    = 256
	maxBranchFilterLen    = 100
	maxReposPerSearch     = 10
	defaultLimitPerRepo   = 10
	maxGlobalSearchLimit  = 100
	defaultGlobalSearchLimit = 30
)

// defaultVersion is the fallback version when not set via -ldflags
const defaultVersion = "0.2.0"

// version is injected at build time via -ldflags="-X main.version=1.0.1"
var version = defaultVersion

// ============================================================================
// Type Definitions
// ============================================================================

// JsonRpcRequest represents the JSON-RPC 2.0 request structure
type JsonRpcRequest struct {
	Jsonrpc string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      any             `json:"id,omitempty"`
}

// CallParams is used for parsing tool call arguments
type CallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// githubClient encapsulates GitHub API HTTP client with authentication
type githubClient struct {
	httpClient *http.Client
	token      string
}

// newGitHubClient creates a new GitHub API client with the given token
func newGitHubClient(token string) *githubClient {
	return &githubClient{
		httpClient: &http.Client{Timeout: githubAPITimeout},
		token:      token,
	}
}

// get executes a GET request to GitHub API with proper headers
func (c *githubClient) get(ctx context.Context, apiPath string, acceptHeader string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Go-MCP-Server")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.token))
	}

	if acceptHeader != "" {
		req.Header.Set("Accept", acceptHeader)
	}

	return c.httpClient.Do(req)
}

// decodeJSON decodes JSON response body into the provided value
func (c *githubClient) decodeJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// doAPI handles the common pattern: build API path → GET → check status → decode JSON
func (c *githubClient) doAPI(ctx context.Context, owner, repo, endpoint, accept string, result any) error {
	apiUrl := apiPath(owner, repo, endpoint)
	resp, err := c.get(ctx, apiUrl, accept)
	if err != nil {
		return err
	}
	if err := checkStatus(resp); err != nil {
		return err
	}
	return c.decodeJSON(resp, result)
}

// doAPIRaw handles the common pattern: build API path → GET → check status → read raw body
func (c *githubClient) doAPIRaw(ctx context.Context, owner, repo, endpoint, accept string) ([]byte, error) {
	apiUrl := apiPath(owner, repo, endpoint)
	resp, err := c.get(ctx, apiUrl, accept)
	if err != nil {
		return nil, err
	}
	if err := checkStatus(resp); err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// toolDef defines a tool with its name, description, and input schema
type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// allTools defines all available tools as Go structs (no JSON string round-trip)
var allTools = []toolDef{
	{
		Name:        "get_tags",
		Description: "Call this tool BEFORE writing any dependency in Cargo.toml/package.json. Returns the latest versions. Use 'limit: 5' to avoid fetching old tags.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":   map[string]string{"type": "string"},
				"limit": map[string]string{"type": "integer"},
			},
			"required": []string{"url"},
		},
	},
	{
		Name:        "get_changelog",
		Description: "Analyze commit messages between versions.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":       map[string]string{"type": "string"},
				"start_tag": map[string]string{"type": "string"},
				"end_tag":   map[string]string{"type": "string"},
			},
			"required": []string{"url", "start_tag", "end_tag"},
		},
	},
	{
		Name:        "get_readme",
		Description: "Read the README to find installation instructions.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]string{"type": "string"},
			},
			"required": []string{"url"},
		},
	},
	{
		Name:        "get_file_tree",
		Description: "Explore the repository structure.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":    map[string]string{"type": "string"},
				"branch": map[string]string{"type": "string"},
			},
			"required": []string{"url"},
		},
	},
	{
		Name:        "get_file_content",
		Description: "Read content of source files.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":    map[string]string{"type": "string"},
				"path":   map[string]string{"type": "string"},
				"branch": map[string]string{"type": "string"},
			},
			"required": []string{"url", "path"},
		},
	},
	{
		Name:        "search_repository",
		Description: "Search for specific code, structs, functions, or text across the repository. Returns file paths AND code snippets where matches are found.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":      map[string]interface{}{"type": "string", "description": "GitHub repository URL"},
				"query":    map[string]interface{}{"type": "string", "description": "Code or text to search (e.g., 'fn main', 'class User', 'dlopen')"},
				"language": map[string]interface{}{"type": "string", "description": "Filter by programming language (e.g., 'Go', 'Python')"},
				"filename": map[string]interface{}{"type": "string", "description": "Filter by filename or path pattern"},
				"author":   map[string]interface{}{"type": "string", "description": "Filter by commit author username"},
				"date":     map[string]interface{}{"type": "string", "description": "Filter by commit date (e.g., '>=2024-01-01', '<=2024-12-31')"},
			},
			"required": []string{"url", "query"},
		},
	},
	{
		Name:        "list_issues",
		Description: "List issues for a repository with filtering options",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":    map[string]interface{}{"type": "string", "description": "GitHub repository URL"},
				"state":  map[string]interface{}{"type": "string", "description": "Filter by state: open, closed, all"},
				"labels": map[string]interface{}{"type": "string", "description": "Filter by labels (comma-separated)"},
				"author": map[string]interface{}{"type": "string", "description": "Filter by issue author username"},
				"limit":  map[string]interface{}{"type": "integer", "description": "Maximum results (1-100, default: 30)"},
			},
			"required": []string{"url"},
		},
	},
	{
		Name:        "list_pull_requests",
		Description: "List pull requests for a repository with filtering options",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":    map[string]interface{}{"type": "string", "description": "GitHub repository URL"},
				"state":  map[string]interface{}{"type": "string", "description": "Filter by state: open, closed, all"},
				"head":   map[string]interface{}{"type": "string", "description": "Filter by head branch"},
				"base":   map[string]interface{}{"type": "string", "description": "Filter by base branch"},
				"author": map[string]interface{}{"type": "string", "description": "Filter by PR author username"},
				"limit":  map[string]interface{}{"type": "integer", "description": "Maximum results (1-100, default: 30)"},
			},
			"required": []string{"url"},
		},
	},
	{
		Name:        "search_multiple_repos",
		Description: "Search for code across multiple GitHub repositories in parallel. Returns aggregated results from all repositories.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repos": map[string]interface{}{
					"type":        "array",
					"items":       map[string]string{"type": "string"},
					"description": "List of GitHub repository URLs (max 10)",
				},
				"query":        map[string]interface{}{"type": "string", "description": "Code or text to search"},
				"language":     map[string]interface{}{"type": "string", "description": "Filter by programming language"},
				"filename":     map[string]interface{}{"type": "string", "description": "Filter by filename pattern"},
				"limit_per_repo": map[string]interface{}{"type": "integer", "description": "Max results per repo (default: 10)"},
			},
			"required": []string{"repos", "query"},
		},
	},
	{
		Name:        "github_global_search",
		Description: "Search code across ALL of GitHub (global search). Use for finding patterns across multiple projects.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "Search query (GitHub search syntax supported)"},
				"limit": map[string]interface{}{"type": "integer", "description": "Max results (1-100, default: 30)"},
			},
			"required": []string{"query"},
		},
	},
}

// ============================================================================
// Global Variables
// ============================================================================

// repoQualifierPattern is used to extract repo: qualifier from search queries
var repoQualifierPattern = regexp.MustCompile(`repo:([^\s]+)`)

// ownerRepoPattern validates owner/repo format
var ownerRepoPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,38}[a-zA-Z0-9])?/[a-zA-Z0-9._-]{1,100}$`)

// validDatePattern validates date filter format
var validDatePattern = regexp.MustCompile(`^([><=]?\d{4}-\d{2}-\d{2}|\d{4}-\d{2}-\d{2}\.\.\d{4}-\d{2}-\d{2})$`)

// ============================================================================
// URL Parsing and Validation
// ============================================================================

// parseGitHubURL validates and extracts owner/repo from a GitHub URL
// Combines URL validation and parsing into a single function
func parseGitHubURL(rawURL string) (owner, repo string, err error) {
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

// ============================================================================
// Helper Functions
// ============================================================================

// apiPath builds a GitHub API URL for a given owner/repo and endpoint.
func apiPath(owner, repo, endpoint string) string {
	return fmt.Sprintf("%s/repos/%s/%s/%s", githubAPI, owner, repo, endpoint)
}

// checkStatus returns an error if the HTTP response status is not 200 OK.
func checkStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return fmt.Errorf("API error: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
}

// resolveRepo validates the URL and returns owner/repo, or a descriptive error.
func resolveRepo(rawURL string) (owner, repo string, err error) {
	if rawURL == "" {
		return "", "", fmt.Errorf("missing required parameter: url")
	}
	return parseGitHubURL(rawURL)
}

// parseGitHubSearchURL extracts owner/repo and query from GitHub search URLs
// Returns:
// - owner, repo, extractedQuery, nil for search URLs with repo: qualifier
// - "", "", query, nil for non-search URLs (caller should use resolveRepo)
// - "", "", "", err for search URLs without repo: qualifier
func parseGitHubSearchURL(rawURL string) (string, string, string, error) {
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
	matches := repoQualifierPattern.FindStringSubmatch(q)
	if len(matches) >= 2 {
		repoValue := matches[1]
		// Validate owner/repo format
		if !ownerRepoPattern.MatchString(repoValue) {
			return "", "", "", fmt.Errorf("invalid repo: qualifier format: %s", repoValue)
		}
		parts := strings.Split(repoValue, "/")
		if len(parts) == 2 {
			// Remove repo: qualifier from query for potential use
			extractedQuery := strings.TrimSpace(repoQualifierPattern.ReplaceAllString(q, ""))
			if extractedQuery == "" {
				return "", "", "", fmt.Errorf("search URL has repo: qualifier but no search query")
			}
			return parts[0], parts[1], extractedQuery, nil
		}
	}

	// Search URL without repo: qualifier
	return "", "", "", fmt.Errorf("search URL must contain repo: qualifier for repository-specific search")
}

// getString helper function to read string arguments from JSONRPC
// Returns (value, ok) to allow callers to detect missing/invalid keys
func getString(args map[string]interface{}, key string) (string, bool) {
	if val, ok := args[key].(string); ok {
		return val, true
	}
	return "", false
}

// truncateWithSave truncates content if it exceeds the limit and saves full content to a temp file
// Returns (truncatedContent, metadata) where metadata contains truncation info
func truncateWithSave(content string, limit int, label string) (string, map[string]interface{}) {
	metadata := make(map[string]interface{})

	if len(content) <= limit {
		return content, metadata
	}

	// Create truncated version
	truncated := content[:limit] + "... \n[TRUNCATED - Full content saved to file]"

	// Save full content to temp file using os.CreateTemp (no directory cleanup needed)
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("git-mcp-%s-*.txt", label))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[WARNING] Failed to create temp file for truncation: %v\n", err)
		return truncated, metadata
	}
	tmpFile.Close() // Close file handle, we'll write to it separately

	if err := os.WriteFile(tmpFile.Name(), []byte(content), 0600); err != nil {
		fmt.Fprintf(os.Stderr, "[WARNING] Failed to save truncated content: %v\n", err)
		return truncated, metadata
	}

	metadata["is_truncated"] = true
	metadata["truncated_file_path"] = tmpFile.Name()

	return truncated, metadata
}

// toolErrorResult creates a CallToolResult with an error message.
func toolErrorResult(message string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": message}},
		"isError": true,
	}
}

// toolSuccessResult creates a CallToolResult with success content.
func toolSuccessResult(content string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": content}},
	}
}

// jsonrpcError creates a JSON-RPC protocol error response.
func jsonrpcError(id any, code int64, message string) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
}

// debugf prints debug messages to stderr
func debugf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
}

// semverDesc sorts version strings in descending SemVer order.
func semverDesc(tags []string) {
	sort.Slice(tags, func(i, j int) bool {
		return compareSemverDesc(tags[i], tags[j])
	})
}

// compareSemverDesc compares two version strings for descending SemVer sort.
func compareSemverDesc(a, b string) bool {
	va, vb := normalizeSemver(a), normalizeSemver(b)
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

func normalizeSemver(v string) string {
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// ============================================================================
// Result Mapping Helpers
// ============================================================================

// formatCommits formats commit entries for changelog output
func formatCommits(commits []struct {
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}) []string {
	result := make([]string, len(commits))
	for i, c := range commits {
		msg := strings.Split(c.Commit.Message, "\n")[0]
		date := strings.Split(c.Commit.Author.Date, "T")[0]
		result[i] = fmt.Sprintf("[%s] %s", date, msg)
	}
	return result
}

// flattenTree extracts file paths from a tree entry list
func flattenTree(tree []struct {
	Path string `json:"path"`
	Type string `json:"type"`
}) []string {
	result := make([]string, len(tree))
	for i, entry := range tree {
		if entry.Type == "tree" {
			result[i] = entry.Path + "/"
		} else {
			result[i] = entry.Path
		}
	}
	return result
}

// mapSearchResults transforms search API items into clean result format
func mapSearchResults(items []struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	HtmlUrl     string `json:"html_url"`
	TextMatches []struct {
		Fragment string `json:"fragment"`
	} `json:"text_matches"`
}) []map[string]interface{} {
	result := make([]map[string]interface{}, len(items))
	for i, item := range items {
		var snippets []string
		for _, match := range item.TextMatches {
			snippets = append(snippets, match.Fragment)
		}
		result[i] = map[string]interface{}{
			"file":     item.Name,
			"path":     item.Path,
			"url":      item.HtmlUrl,
			"snippets": snippets,
		}
	}
	return result
}

// ============================================================================
// Tool Implementation Functions
// ============================================================================

// getTags fetches Git tags from a repository and sorts them by SemVer
func getTags(ctx context.Context, client *githubClient, link string, limit float64) (interface{}, error) {
	debugf("Fetching tags for: %s (limit: %v)", link, limit)

	owner, repo, err := resolveRepo(link)
	if err != nil {
		return nil, err
	}
	_ = owner // suppress unused variable warning (owner/repo not needed for git ls-remote)
	_ = repo

	// Add 30s timeout context to prevent resource exhaustion
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "git", "ls-remote", "--tags", "--refs", "--", link)
	out, err := cmd.Output()
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("git ls-remote timed out after 30s")
		}
		return nil, fmt.Errorf("git ls-remote error: %v", err)
	}

	lines := strings.Split(string(out), "\n")
	var tags []string
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 {
			tag := strings.TrimPrefix(parts[1], "refs/tags/")
			tags = append(tags, tag)
		}
	}

	// Sort tags using Semantic Versioning (descending)
	semverDesc(tags)

	if limit > 0 && int(limit) < len(tags) {
		tags = tags[:int(limit)]
	}

	return map[string]interface{}{
		"repository":    link,
		"count":         len(tags),
		"limit_applied": limit,
		"tags":          tags,
	}, nil
}

// getChangelog fetches commit messages between two tags
func getChangelog(ctx context.Context, client *githubClient, link, v1, v2 string) (interface{}, error) {
	debugf("Fetching changelog: %s...%s", v1, v2)

	// Check required parameters
	if v1 == "" {
		return nil, fmt.Errorf("missing required parameter: start_tag")
	}
	if v2 == "" {
		return nil, fmt.Errorf("missing required parameter: end_tag")
	}

	owner, repo, err := resolveRepo(link)
	if err != nil {
		return nil, err
	}

	var result struct {
		Commits []struct {
			Commit struct {
				Message string `json:"message"`
				Author  struct {
					Date string `json:"date"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"commits"`
	}

	if err := client.doAPI(ctx, owner, repo, fmt.Sprintf("compare/%s...%s", v1, v2), "", &result); err != nil {
		return nil, err
	}

	summaries := formatCommits(result.Commits)

	content := strings.Join(summaries, "\n")
	truncatedContent, metadata := truncateWithSave(content, maxChangelogEntries*50, "changelog_"+v1+"_to_"+v2)

	response := map[string]interface{}{
		"repository": link,
		"from":       v1,
		"to":         v2,
		"changes":    truncatedContent,
	}

	// Add metadata if truncation occurred
	maps.Copy(response, metadata)

	return response, nil
}

// getReadme fetches the README file from a GitHub repository
func getReadme(ctx context.Context, client *githubClient, link string) (interface{}, error) {
	debugf("Fetching README: %s", link)

	owner, repo, err := resolveRepo(link)
	if err != nil {
		return nil, err
	}

	body, err := client.doAPIRaw(ctx, owner, repo, "readme", "application/vnd.github.raw")
	if err != nil {
		return nil, err
	}

	content := string(body)

	truncatedContent, metadata := truncateWithSave(content, maxReadmeLength, "readme")

	result := map[string]interface{}{
		"repository": link,
		"type":       "readme",
		"content":    truncatedContent,
	}

	// Add metadata if truncation occurred
	maps.Copy(result, metadata)

	return result, nil
}

// getFileTree fetches the file tree structure of a repository
func getFileTree(ctx context.Context, client *githubClient, link, branch string) (interface{}, error) {
	debugf("Fetching Tree: %s", link)

	owner, repo, err := resolveRepo(link)
	if err != nil {
		return nil, err
	}

	if branch == "" {
		branch = "HEAD"
	}

	var result struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}

	if err := client.doAPI(ctx, owner, repo, fmt.Sprintf("git/trees/%s?recursive=1", branch), "", &result); err != nil {
		return nil, err
	}

	files := flattenTree(result.Tree)

	response := map[string]interface{}{
		"repository": link,
		"ref":        branch,
		"files":      files,
	}

	// Truncate if more than maxFileTreeEntries
	fileListStr := strings.Join(files, "\n")
	truncatedContent, metadata := truncateWithSave(fileListStr, maxFileTreeEntries*50, "filetree")

	// Update files with truncated content
	if metadata["is_truncated"] != nil {
		lines := strings.Split(truncatedContent, "\n")
		response["files"] = lines
	}

	// Add metadata if truncation occurred
	maps.Copy(response, metadata)

	return response, nil
}

// getFileContent reads the content of a specific file
func getFileContent(ctx context.Context, client *githubClient, link, filePath, branch string) (interface{}, error) {
	debugf("Reading file: %s @ %s", filePath, link)

	// Check required parameters
	if filePath == "" {
		return nil, fmt.Errorf("missing required parameter: path")
	}

	owner, repo, err := resolveRepo(link)
	if err != nil {
		return nil, err
	}

	if branch == "" {
		branch = "HEAD"
	}
	cleanPath := strings.TrimPrefix(filePath, "/")

	body, err := client.doAPIRaw(ctx, owner, repo, fmt.Sprintf("contents/%s?ref=%s", cleanPath, branch), "application/vnd.github.raw")
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	content := string(body)

	truncatedContent, metadata := truncateWithSave(content, maxFileContentLength, "file_"+strings.ReplaceAll(cleanPath, "/", "_"))

	result := map[string]interface{}{
		"repository": link,
		"path":       cleanPath,
		"ref":        branch,
		"content":    truncatedContent,
	}

	// Add metadata if truncation occurred
	maps.Copy(result, metadata)

	return result, nil
}

// buildSearchQuery constructs a GitHub Search API query string with filters
func buildSearchQuery(query, owner, repo, language, filename, author, date string) string {
	q := fmt.Sprintf("%s repo:%s/%s", query, owner, repo)

	if language != "" {
		q += fmt.Sprintf(" language:%s", language)
	}

	if filename != "" {
		q += fmt.Sprintf(" filename:%s", filename)
	}

	if author != "" {
		q += fmt.Sprintf(" author:%s", author)
	}

	if date != "" {
		q += fmt.Sprintf(" %s", date)
	}

	return q
}

// validateSearchFilters validates the length and format of search filter parameters
func validateSearchFilters(language, filename, author, date string) error {
	if len(language) > maxLanguageFilterLen {
		return fmt.Errorf("language filter exceeds maximum length of %d characters", maxLanguageFilterLen)
	}

	if len(filename) > maxFilenameFilterLen {
		return fmt.Errorf("filename filter exceeds maximum length of %d characters", maxFilenameFilterLen)
	}

	if len(author) > maxAuthorFilterLen {
		return fmt.Errorf("author filter exceeds maximum length of %d characters", maxAuthorFilterLen)
	}

	if date != "" {
		if len(date) > maxDateFilterLen {
			return fmt.Errorf("date filter exceeds maximum length of %d characters", maxDateFilterLen)
		}
		// Validate date format: >YYYY-MM-DD, <YYYY-MM-DD, YYYY-MM-DD..YYYY-MM-DD
		if !validDatePattern.MatchString(date) {
			return fmt.Errorf("invalid date format. Use: >YYYY-MM-DD, <YYYY-MM-DD, or YYYY-MM-DD..YYYY-MM-DD")
		}
	}

	return nil
}

// searchRepository searches for code within a GitHub repository and returns code snippets
func searchRepository(ctx context.Context, client *githubClient, link, query, language, filename, author, date string, limit int) (interface{}, error) {
	debugf("Searching '%s' in %s", query, link)

	// Clamp limit to GitHub API max
	if limit > 100 {
		limit = 100
	}

	// Check if link is a search URL
	owner, repo, extractedQuery, err := parseGitHubSearchURL(link)
	if err != nil {
		return nil, err
	}
	if owner != "" && repo != "" {
		// Search URL with repo: qualifier - use extracted values
		if extractedQuery != "" && query == "" {
			query = extractedQuery
		}
	} else {
		// Not a search URL or no repo: qualifier - use resolveRepo
		owner, repo, err = resolveRepo(link)
		if err != nil {
			return nil, err
		}
	}

	// Validate query after extracting from search URL
	if query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	if len(query) > maxSearchQueryLen {
		return nil, fmt.Errorf("query exceeds maximum length of %d characters", maxSearchQueryLen)
	}

	if err := validateSearchFilters(language, filename, author, date); err != nil {
		return nil, err
	}

	q := buildSearchQuery(query, owner, repo, language, filename, author, date)

	if limit <= 0 {
		limit = 10
	}
	params := url.Values{}
	params.Add("q", q)
	params.Add("per_page", fmt.Sprintf("%d", limit))

	apiUrl := fmt.Sprintf("%s/search/code?%s", githubAPI, params.Encode())

	resp, err := client.get(ctx, apiUrl, "application/vnd.github.v3.text-match+json")
	if err != nil {
		return nil, err
	}

	if err := checkStatus(resp); err != nil {
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		detail := "unknown"
		if readErr == nil && len(bodyBytes) > 0 {
			detail = strings.TrimSpace(string(bodyBytes))
		}
		return nil, fmt.Errorf("search API error: %d. Detail: %s", resp.StatusCode, detail)
	}

	// JSON structure to capture GitHub's response with text matches
	var result struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			Name        string `json:"name"`
			Path        string `json:"path"`
			HtmlUrl     string `json:"html_url"`
			TextMatches []struct {
				Fragment string `json:"fragment"` // Code snippet where match was found
			} `json:"text_matches"`
		} `json:"items"`
	}

	if err := client.decodeJSON(resp, &result); err != nil {
		return nil, err
	}

	results := mapSearchResults(result.Items)

	return map[string]interface{}{
		"repository":  link,
		"query":       query,
		"total_found": result.TotalCount,
		"results":     results,
	}, nil
}

type searchResult struct {
	repo   string
	result interface{}
	err    error
}

func searchMultipleRepos(ctx context.Context, client *githubClient, repos []string, query, language, filename string, limitPerRepo int) (interface{}, error) {
	// L1: Early query validation
	if query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}
	if len(query) > maxSearchQueryLen {
		return nil, fmt.Errorf("query exceeds maximum length of %d characters", maxSearchQueryLen)
	}

	if len(repos) == 0 {
		return nil, fmt.Errorf("repos array cannot be empty")
	}

	if len(repos) > maxReposPerSearch {
		return nil, fmt.Errorf("too many repositories: %d (max: %d)", len(repos), maxReposPerSearch)
	}

	for i, repo := range repos {
		if repo == "" {
			return nil, fmt.Errorf("repository URL at index %d is empty", i)
		}
		if _, _, err := parseGitHubURL(repo); err != nil {
			return nil, fmt.Errorf("invalid repository URL at index %d: %w", i, err)
		}
	}

	// L2: Deduplicate repos
	seen := make(map[string]bool)
	uniqueRepos := []string{}
	for _, repo := range repos {
		if !seen[repo] {
			seen[repo] = true
			uniqueRepos = append(uniqueRepos, repo)
		}
	}
	repos = uniqueRepos

	if limitPerRepo < 1 {
		limitPerRepo = defaultLimitPerRepo
	}

	resultChan := make(chan searchResult, len(repos))
	var wg sync.WaitGroup

	// M2: Add semaphore to limit concurrency to 3 requests
	sem := make(chan struct{}, 3)

	for _, repo := range repos {
		wg.Add(1)
		go func(repoURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result, err := searchRepository(ctx, client, repoURL, query, language, filename, "", "", limitPerRepo)
			resultChan <- searchResult{repo: repoURL, result: result, err: err}
		}(repo)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	var allResults []map[string]interface{}
	var errors []string
	repoResults := make(map[string]interface{})

	for res := range resultChan {
		if res.err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", res.repo, res.err))
			repoResults[res.repo] = map[string]interface{}{
				"success": false,
				"error":   res.err.Error(),
			}
		} else {
			if data, ok := res.result.(map[string]interface{}); ok {
				// M1: Safe type assertion with comma-ok idiom
				results, ok := data["results"].([]map[string]interface{})
				if !ok {
					errors = append(errors, fmt.Sprintf("%s: unexpected result format", res.repo))
					repoResults[res.repo] = map[string]interface{}{
						"success": false,
						"error":   "unexpected result format",
					}
					continue
				}
				for _, r := range results {
					r["repository"] = res.repo
					allResults = append(allResults, r)
				}
				repoResults[res.repo] = map[string]interface{}{
					"success":     true,
					"total_found": data["total_found"],
					"count":       len(results),
				}
			}
		}
	}

	response := map[string]interface{}{
		"query":         query,
		"repos_searched": len(repos),
		"repos":         repos,
		"total_results": len(allResults),
		"results":       allResults,
		"repo_status":   repoResults,
	}

	if len(errors) > 0 {
		response["errors"] = errors
	}

	return response, nil
}

// githubGlobalSearch searches code across ALL of GitHub using the global search API
func githubGlobalSearch(ctx context.Context, client *githubClient, query string, limit int) (interface{}, error) {
	debugf("Global search for: '%s' (limit: %d)", query, limit)

	if query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	if len(query) > maxSearchQueryLen {
		return nil, fmt.Errorf("query exceeds maximum length of %d characters", maxSearchQueryLen)
	}

	if limit <= 0 {
		limit = defaultGlobalSearchLimit
	}
	if limit > maxGlobalSearchLimit {
		limit = maxGlobalSearchLimit
	}

	params := url.Values{}
	params.Add("q", query)
	params.Add("per_page", fmt.Sprintf("%d", limit))

	apiUrl := fmt.Sprintf("%s/search/code?%s", githubAPI, params.Encode())

	resp, err := client.get(ctx, apiUrl, "application/vnd.github.v3.text-match+json")
	if err != nil {
		return nil, err
	}

	if err := checkStatus(resp); err != nil {
		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		detail := "unknown"
		if readErr == nil && len(bodyBytes) > 0 {
			detail = strings.TrimSpace(string(bodyBytes))
		}
		return nil, fmt.Errorf("search API error: %d. Detail: %s", resp.StatusCode, detail)
	}

	// JSON structure to capture GitHub's response with text matches
	var result struct {
		TotalCount int `json:"total_count"`
		Items      []struct {
			Name        string `json:"name"`
			Path        string `json:"path"`
			HtmlUrl     string `json:"html_url"`
			Repository  struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			TextMatches []struct {
				Fragment string `json:"fragment"`
			} `json:"text_matches"`
		} `json:"items"`
	}

	if err := client.decodeJSON(resp, &result); err != nil {
		return nil, err
	}

	// Transform results to include repository information
	results := make([]map[string]interface{}, len(result.Items))
	for i, item := range result.Items {
		var snippets []string
		for _, match := range item.TextMatches {
			snippets = append(snippets, match.Fragment)
		}
		results[i] = map[string]interface{}{
			"file":       item.Name,
			"path":       item.Path,
			"url":        item.HtmlUrl,
			"repository": item.Repository.FullName,
			"snippets":   snippets,
		}
	}

	return map[string]interface{}{
		"query":       query,
		"total_found": result.TotalCount,
		"limit":       limit,
		"results":     results,
	}, nil
}

// mapIssues transforms GitHub API issue response to clean format
func mapIssues(issues []struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	User      *struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	HtmlUrl   string `json:"html_url"`
}) []map[string]interface{} {
	result := make([]map[string]interface{}, len(issues))
	for i, issue := range issues {
		labels := make([]string, len(issue.Labels))
		for j, label := range issue.Labels {
			labels[j] = label.Name
		}
		author := ""
		if issue.User != nil {
			author = issue.User.Login
		}
		result[i] = map[string]interface{}{
			"number":     issue.Number,
			"title":      issue.Title,
			"state":      issue.State,
			"author":     author,
			"created_at": issue.CreatedAt,
			"updated_at": issue.UpdatedAt,
			"labels":     labels,
			"url":        issue.HtmlUrl,
		}
	}
	return result
}

// mapPullRequests transforms GitHub API PR response to clean format
func mapPullRequests(prs []struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	User      *struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Head      struct {
		Ref string `json:"ref"`
	} `json:"head"`
	Base      struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Mergeable *bool  `json:"mergeable"`
	Merged    bool   `json:"merged"`
	HtmlUrl   string `json:"html_url"`
}) []map[string]interface{} {
	result := make([]map[string]interface{}, len(prs))
	for i, pr := range prs {
		var mergeable interface{}
		if pr.Mergeable != nil {
			mergeable = *pr.Mergeable
		} else {
			mergeable = nil
		}
		author := ""
		if pr.User != nil {
			author = pr.User.Login
		}
		result[i] = map[string]interface{}{
			"number":      pr.Number,
			"title":       pr.Title,
			"state":       pr.State,
			"author":      author,
			"created_at":  pr.CreatedAt,
			"updated_at":  pr.UpdatedAt,
			"head_branch": pr.Head.Ref,
			"base_branch": pr.Base.Ref,
			"mergeable":   mergeable,
			"merged":      pr.Merged,
			"url":         pr.HtmlUrl,
		}
	}
	return result
}

// validateState validates the state parameter for issues/PRs API
func validateState(state string) error {
	if state == "" {
		return nil
	}
	validStates := map[string]bool{"open": true, "closed": true, "all": true}
	if !validStates[state] {
		return fmt.Errorf("invalid state: %s. Must be one of: open, closed, all", state)
	}
	return nil
}

// validateListLimit validates the limit parameter (1-100)
func validateListLimit(limit int) error {
	if limit < 1 || limit > maxListLimit {
		return fmt.Errorf("limit must be between 1 and %d", maxListLimit)
	}
	return nil
}

// listIssues fetches issues for a repository with filtering options
func listIssues(ctx context.Context, client *githubClient, link, state, labels, author string, limit int) (interface{}, error) {
	debugf("Fetching issues for: %s (state: %s, labels: %s, author: %s, limit: %d)", link, state, labels, author, limit)

	if err := validateState(state); err != nil {
		return nil, err
	}

	if len(labels) > maxLabelsFilterLen {
		return nil, fmt.Errorf("labels filter exceeds maximum length of %d characters", maxLabelsFilterLen)
	}

	if len(author) > maxAuthorFilterLen {
		return nil, fmt.Errorf("author filter exceeds maximum length of %d characters", maxAuthorFilterLen)
	}

	if err := validateListLimit(limit); err != nil {
		return nil, err
	}

	owner, repo, err := resolveRepo(link)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Add("per_page", fmt.Sprintf("%d", limit))

	if state != "" {
		params.Add("state", state)
	}
	if labels != "" {
		params.Add("labels", labels)
	}
	if author != "" {
		params.Add("creator", author)
	}

	endpoint := "issues?" + params.Encode()

	var issues []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		State     string `json:"state"`
		User      *struct {
			Login string `json:"login"`
		} `json:"user"`
		CreatedAt string   `json:"created_at"`
		UpdatedAt string   `json:"updated_at"`
		Labels    []struct {
			Name string `json:"name"`
		} `json:"labels"`
		HtmlUrl string `json:"html_url"`
	}

	if err := client.doAPI(ctx, owner, repo, endpoint, "", &issues); err != nil {
		return nil, err
	}

	mappedIssues := mapIssues(issues)

	return map[string]interface{}{
		"repository": link,
		"state":      state,
		"labels":     labels,
		"author":     author,
		"count":      len(mappedIssues),
		"issues":     mappedIssues,
	}, nil
}



// validateBranchFilter validates branch filter parameters
func validateBranchFilter(filter, name string) error {
	if len(filter) > maxBranchFilterLen {
		return fmt.Errorf("%s filter exceeds maximum length of %d characters", name, maxBranchFilterLen)
	}
	return nil
}

// listPullRequests fetches pull requests for a repository with filtering options
func listPullRequests(ctx context.Context, client *githubClient, link, state, head, base, author string, limit int) (interface{}, error) {
	debugf("Fetching pull requests for: %s (state: %s, head: %s, base: %s, author: %s, limit: %d)", link, state, head, base, author, limit)

	if err := validateState(state); err != nil {
		return nil, err
	}

	if err := validateBranchFilter(head, "head"); err != nil {
		return nil, err
	}

	if err := validateBranchFilter(base, "base"); err != nil {
		return nil, err
	}

	if len(author) > maxAuthorFilterLen {
		return nil, fmt.Errorf("author filter exceeds maximum length of %d characters", maxAuthorFilterLen)
	}

	if err := validateListLimit(limit); err != nil {
		return nil, err
	}

	owner, repo, err := resolveRepo(link)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Add("per_page", fmt.Sprintf("%d", limit))

	if state != "" {
		params.Add("state", state)
	}
	if head != "" {
		params.Add("head", head)
	}
	if base != "" {
		params.Add("base", base)
	}
	if author != "" {
		params.Add("creator", author)
	}

	endpoint := "pulls?" + params.Encode()

	var prs []struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		State     string `json:"state"`
		User      *struct {
			Login string `json:"login"`
		} `json:"user"`
		CreatedAt string   `json:"created_at"`
		UpdatedAt string   `json:"updated_at"`
		Head      struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base      struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Mergeable *bool  `json:"mergeable"`
		Merged    bool   `json:"merged"`
		HtmlUrl   string `json:"html_url"`
	}

	if err := client.doAPI(ctx, owner, repo, endpoint, "", &prs); err != nil {
		return nil, err
	}

	mappedPRs := mapPullRequests(prs)

	return map[string]interface{}{
		"repository": link,
		"state":      state,
		"head":       head,
		"base":       base,
		"author":     author,
		"count":      len(mappedPRs),
		"pull_requests": mappedPRs,
	}, nil
}

// ============================================================================
// Request Handlers
// ============================================================================

// handleInitialize handles the initialize method
func handleInitialize() map[string]interface{} {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]interface{}{"name": "go-git-mcp", "version": version},
	}
}

// handleToolsList handles the tools/list method
func handleToolsList() map[string]interface{} {
	// Convert toolDef structs to map[string]interface{} for JSON response
	tools := make([]map[string]interface{}, len(allTools))
	for i, tool := range allTools {
		tools[i] = map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		}
	}
	return map[string]interface{}{"tools": tools}
}

// handleToolCall handles the tools/call method
func handleToolCall(ctx context.Context, client *githubClient, name string, args map[string]interface{}) map[string]interface{} {
	data, err := dispatchTool(ctx, client, name, args)
	if err != nil {
		return toolErrorResult(err.Error())
	}
	jsonBytes, marshalErr := json.Marshal(data)
	if marshalErr != nil {
		return toolErrorResult(fmt.Sprintf("failed to marshal response: %v", marshalErr))
	}
	return toolSuccessResult(string(jsonBytes))
}

// dispatchTool routes the tool call to the appropriate implementation.
func dispatchTool(ctx context.Context, client *githubClient, name string, args map[string]interface{}) (interface{}, error) {
	urlStr, _ := getString(args, "url")
	switch name {
	case "get_tags":
		limit := 0.0
		if l, ok := args["limit"].(float64); ok {
			limit = l
		}
		return getTags(ctx, client, urlStr, limit)
	case "get_changelog":
		startTag, _ := getString(args, "start_tag")
		endTag, _ := getString(args, "end_tag")
		return getChangelog(ctx, client, urlStr, startTag, endTag)
	case "get_readme":
		return getReadme(ctx, client, urlStr)
	case "get_file_tree":
		branch, _ := getString(args, "branch")
		return getFileTree(ctx, client, urlStr, branch)
	case "get_file_content":
		path, _ := getString(args, "path")
		branch, _ := getString(args, "branch")
		return getFileContent(ctx, client, urlStr, path, branch)
	case "search_repository":
		query, _ := getString(args, "query")
		language, _ := getString(args, "language")
		filename, _ := getString(args, "filename")
		author, _ := getString(args, "author")
		date, _ := getString(args, "date")
		limit := 10
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		return searchRepository(ctx, client, urlStr, query, language, filename, author, date, limit)
	case "list_issues":
		state, _ := getString(args, "state")
		labels, _ := getString(args, "labels")
		author, _ := getString(args, "author")
		limit := defaultListLimit
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		return listIssues(ctx, client, urlStr, state, labels, author, limit)
	case "list_pull_requests":
		state, _ := getString(args, "state")
		head, _ := getString(args, "head")
		base, _ := getString(args, "base")
		author, _ := getString(args, "author")
		limit := defaultListLimit
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		return listPullRequests(ctx, client, urlStr, state, head, base, author, limit)
	case "search_multiple_repos":
		reposInterface, ok := args["repos"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("repos must be an array of strings")
		}
		repos := make([]string, len(reposInterface))
		for i, r := range reposInterface {
			if s, ok := r.(string); ok {
				repos[i] = s
			} else {
				return nil, fmt.Errorf("repos[%d] must be a string", i)
			}
		}
		query, _ := getString(args, "query")
		language, _ := getString(args, "language")
		filename, _ := getString(args, "filename")
		limitPerRepo := defaultLimitPerRepo
		if l, ok := args["limit_per_repo"].(float64); ok && l > 0 {
			limitPerRepo = int(l)
		}
		return searchMultipleRepos(ctx, client, repos, query, language, filename, limitPerRepo)
	case "github_global_search":
		query, _ := getString(args, "query")
		limit := defaultGlobalSearchLimit
		if l, ok := args["limit"].(float64); ok && l > 0 {
			limit = int(l)
		}
		return githubGlobalSearch(ctx, client, query, limit)
	default:
		return nil, fmt.Errorf("tool %q not found", name)
	}
}

// writeResponse writes a JSON-RPC response to stdout
func writeResponse(response map[string]interface{}) {
	respBytes, err := json.Marshal(response)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] failed to marshal response: %v\n", err)
		return
	}
	fmt.Println(string(respBytes))
}

// handleNotification processes notifications (requests without an ID).
func handleNotification(req JsonRpcRequest) {
	if req.Method == "notifications/initialized" {
		fmt.Fprintf(os.Stderr, "[INFO] Client initialized.\n")
	}
}

// handleRequest routes a JSON-RPC request and returns the response map.
func handleRequest(ctx context.Context, client *githubClient, req *JsonRpcRequest) map[string]interface{} {
	var result interface{}
	switch req.Method {
	case "initialize":
		result = handleInitialize()
	case "tools/list":
		result = handleToolsList()
	case "tools/call":
		result = handleToolsCallRequest(ctx, client, req.Params)
	default:
		return jsonrpcError(req.ID, -32601, fmt.Sprintf("Method %q not found", req.Method))
	}
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"result":  result,
	}
}

// handleToolsCallRequest parses the tools/call params and delegates to handleToolCall.
func handleToolsCallRequest(ctx context.Context, client *githubClient, params json.RawMessage) interface{} {
	var p CallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return toolErrorResult("Invalid params: " + err.Error())
	}
	return handleToolCall(ctx, client, p.Name, p.Arguments)
}

// serveStdio handles the main stdio loop for JSON-RPC communication
func serveStdio(client *githubClient) {
	scanner := bufio.NewScanner(os.Stdin)
	// Set max input line size to maxScannerBufferSize to prevent resource exhaustion
	scanner.Buffer(make([]byte, 0), maxScannerBufferSize)

	ctx := context.Background()

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var req JsonRpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Invalid JSON: %v | Input: %s\n", err, line)
			writeResponse(jsonrpcError(nil, -32700, "Parse error"))
			continue
		}

		if req.ID == nil {
			handleNotification(req)
			continue
		}

		writeResponse(handleRequest(ctx, client, &req))
	}
}

// ============================================================================
// Main Entry Point
// ============================================================================

func main() {
	// Add --version flag support
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("git-mcp version %s\n", version)
		os.Exit(0)
	}

	// Catch panic (similar to std::panic::set_hook in Rust)
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[FATAL CRASH] Error: %v\n", r)
		}
	}()

	// Create GitHub API client with token from environment
	token := os.Getenv("GITHUB_TOKEN")
	client := newGitHubClient(token)

	// Log auth status once at startup
	if token != "" {
		fmt.Fprintf(os.Stderr, "[INFO] Using GITHUB_TOKEN for authentication.\n")
	} else {
		fmt.Fprintf(os.Stderr, "[INFO] No GITHUB_TOKEN. Using unauthenticated requests.\n")
	}

	// Start stdio server loop
	serveStdio(client)
}
