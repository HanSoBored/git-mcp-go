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
	"sort"
	"strings"
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
				"url":   map[string]interface{}{"type": "string", "description": "GitHub repository URL"},
				"query": map[string]interface{}{"type": "string", "description": "Code or text to search (e.g., 'fn main', 'class User', 'dlopen')"},
			},
			"required": []string{"url", "query"},
		},
	},
}

// ============================================================================
// Global Variables
// ============================================================================

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
		return nil, fmt.Errorf("Failed to read file: %w", err)
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

// searchRepository searches for code within a GitHub repository and returns code snippets
func searchRepository(ctx context.Context, client *githubClient, link, query string) (interface{}, error) {
	debugf("Searching '%s' in %s", query, link)

	// Check required parameters
	if query == "" {
		return nil, fmt.Errorf("missing required parameter: query")
	}

	owner, repo, err := resolveRepo(link)
	if err != nil {
		return nil, err
	}

	// Format query for GitHub Search API: "search_term repo:owner/repo"
	q := fmt.Sprintf("%s repo:%s/%s", query, owner, repo)

	params := url.Values{}
	params.Add("q", q)
	params.Add("per_page", "10") // Limit to 10 results to avoid overwhelming AI context

	apiUrl := fmt.Sprintf("%s/search/code?%s", githubAPI, params.Encode())

	resp, err := client.get(ctx, apiUrl, "application/vnd.github.v3.text-match+json")
	if err != nil {
		return nil, err
	}

	if err := checkStatus(resp); err != nil {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("Search API Error: %d. Detail: %s (Note: GitHub Search API requires GITHUB_TOKEN)", resp.StatusCode, string(bodyBytes))
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
		return toolErrorResult(fmt.Sprintf("Failed to marshal response: %v", marshalErr))
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
		return searchRepository(ctx, client, urlStr, query)
	default:
		return nil, fmt.Errorf("tool %q not found", name)
	}
}

// writeResponse writes a JSON-RPC response to stdout
func writeResponse(response map[string]interface{}) {
	respBytes, err := json.Marshal(response)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to marshal response: %v\n", err)
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
