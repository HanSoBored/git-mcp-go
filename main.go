package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

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

var githubUrlRegex = regexp.MustCompile(`github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)

// parseGithubUrl extracts owner and repo from a GitHub URL
func parseGithubUrl(link string) (string, string, error) {
	matches := githubUrlRegex.FindStringSubmatch(link)
	if len(matches) < 3 {
		return "", "", fmt.Errorf("Invalid GitHub URL: %s", link)
	}
	return matches[1], matches[2], nil
}

// doGitHubRequest creates and executes an HTTP request to GitHub API
func doGitHubRequest(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "Go-MCP-Server")

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		fmt.Fprintf(os.Stderr, "[DEBUG] Using GITHUB_TOKEN for authentication.\n")
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	} else {
		fmt.Fprintf(os.Stderr, "[DEBUG] No GITHUB_TOKEN found. Using unauthenticated requests (Rate Limit: 60/hr).\n")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	return client.Do(req)
}

// getTags fetches Git tags from a repository and sorts them by SemVer
func getTags(link string, limit float64) (interface{}, error) {
	fmt.Fprintf(os.Stderr, "[DEBUG] Fetching tags for: %s (limit: %v)\n", link, limit)

	cmd := exec.Command("git", "ls-remote", "--tags", "--refs", link)
	out, err := cmd.Output()
	if err != nil {
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
	sort.Slice(tags, func(i, j int) bool {
		v1, v2 := tags[i], tags[j]
		if !strings.HasPrefix(v1, "v") {
			v1 = "v" + v1
		}
		if !strings.HasPrefix(v2, "v") {
			v2 = "v" + v2
		}

		valid1, valid2 := semver.IsValid(v1), semver.IsValid(v2)

		if valid1 && valid2 {
			return semver.Compare(v1, v2) > 0 // Descending
		}
		if valid1 && !valid2 {
			return true
		}
		if !valid1 && valid2 {
			return false
		}
		return tags[i] > tags[j] // Fallback string comparison
	})

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
func getChangelog(link, v1, v2 string) (interface{}, error) {
	fmt.Fprintf(os.Stderr, "[DEBUG] Fetching changelog: %s...%s\n", v1, v2)
	owner, repo, err := parseGithubUrl(link)
	if err != nil {
		return nil, err
	}

	apiUrl := fmt.Sprintf("https://api.github.com/repos/%s/%s/compare/%s...%s", owner, repo, v1, v2)
	req, _ := http.NewRequest("GET", apiUrl, nil)
	resp, err := doGitHubRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API Error: %d", resp.StatusCode)
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

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var summaries []string
	for _, c := range result.Commits {
		msg := strings.Split(c.Commit.Message, "\n")[0]
		date := strings.Split(c.Commit.Author.Date, "T")[0]
		summaries = append(summaries, fmt.Sprintf("[%s] %s", date, msg))
	}

	return map[string]interface{}{
		"repository": link,
		"from":       v1,
		"to":         v2,
		"changes":    summaries,
	}, nil
}

// getReadme fetches the README file from a GitHub repository
func getReadme(link string) (interface{}, error) {
	fmt.Fprintf(os.Stderr, "[DEBUG] Fetching README: %s\n", link)
	owner, repo, err := parseGithubUrl(link)
	if err != nil {
		return nil, err
	}

	apiUrl := fmt.Sprintf("https://api.github.com/repos/%s/%s/readme", owner, repo)
	req, _ := http.NewRequest("GET", apiUrl, nil)
	req.Header.Set("Accept", "application/vnd.github.raw")

	resp, err := doGitHubRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Error: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	content := string(body)

	if len(content) > 20000 {
		content = content[:20000] + "... [TRUNCATED]"
	}

	return map[string]interface{}{
		"repository": link,
		"type":       "readme",
		"content":    content,
	}, nil
}

// getFileTree fetches the file tree structure of a repository
func getFileTree(link, branch string) (interface{}, error) {
	fmt.Fprintf(os.Stderr, "[DEBUG] Fetching Tree: %s\n", link)
	owner, repo, err := parseGithubUrl(link)
	if err != nil {
		return nil, err
	}

	if branch == "" {
		branch = "HEAD"
	}

	apiUrl := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, branch)
	req, _ := http.NewRequest("GET", apiUrl, nil)
	resp, err := doGitHubRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Error: %d", resp.StatusCode)
	}

	var result struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var files []string
	for _, item := range result.Tree {
		if item.Type == "tree" {
			files = append(files, item.Path+"/")
		} else {
			files = append(files, item.Path)
		}
	}

	if len(files) > 1000 {
		files = append(files[:1000], "... [TRUNCATED]")
	}

	return map[string]interface{}{
		"repository": link,
		"ref":        branch,
		"files":      files,
	}, nil
}

// getFileContent reads the content of a specific file
func getFileContent(link, filePath, branch string) (interface{}, error) {
	fmt.Fprintf(os.Stderr, "[DEBUG] Reading file: %s @ %s\n", filePath, link)
	owner, repo, err := parseGithubUrl(link)
	if err != nil {
		return nil, err
	}

	if branch == "" {
		branch = "HEAD"
	}
	cleanPath := strings.TrimPrefix(filePath, "/")

	apiUrl := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s", owner, repo, cleanPath, branch)
	req, _ := http.NewRequest("GET", apiUrl, nil)
	req.Header.Set("Accept", "application/vnd.github.raw")

	resp, err := doGitHubRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Failed to read file: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	content := string(body)

	isTruncated := false
	if len(content) > 30000 {
		content = content[:30000] + "... \n[TRUNCATED]"
		isTruncated = true
	}

	return map[string]interface{}{
		"repository":   link,
		"path":         cleanPath,
		"ref":          branch,
		"is_truncated": isTruncated,
		"content":      content,
	}, nil
}

// searchRepository searches for code within a GitHub repository and returns code snippets
func searchRepository(link, query string) (interface{}, error) {
	fmt.Fprintf(os.Stderr, "[DEBUG] Searching '%s' in %s\n", query, link)

	owner, repo, err := parseGithubUrl(link)
	if err != nil {
		return nil, err
	}

	// Format query for GitHub Search API: "search_term repo:owner/repo"
	q := fmt.Sprintf("%s repo:%s/%s", query, owner, repo)

	params := url.Values{}
	params.Add("q", q)
	params.Add("per_page", "10") // Limit to 10 results to avoid overwhelming AI context

	apiUrl := fmt.Sprintf("https://api.github.com/search/code?%s", params.Encode())

	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return nil, err
	}

	// ✨ MAGIC: This header tells GitHub to return text snippets where the match was found
	// This is extremely useful for AI to understand context without fetching full files
	req.Header.Set("Accept", "application/vnd.github.v3.text-match+json")

	resp, err := doGitHubRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
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

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Map search results for clean AI consumption
	var results []map[string]interface{}
	for _, item := range result.Items {
		var snippets []string
		for _, match := range item.TextMatches {
			snippets = append(snippets, match.Fragment)
		}

		results = append(results, map[string]interface{}{
			"file":     item.Name,
			"path":     item.Path,
			"url":      item.HtmlUrl,
			"snippets": snippets, // AI can read code snippets directly here!
		})
	}

	return map[string]interface{}{
		"repository":  link,
		"query":       query,
		"total_found": result.TotalCount,
		"results":     results,
	}, nil
}

// getString helper function to read string arguments from JSONRPC
func getString(args map[string]interface{}, key string) string {
	if val, ok := args[key].(string); ok {
		return val
	}
	return ""
}

func main() {
	// Catch panic (similar to std::panic::set_hook in Rust)
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[FATAL CRASH] Error: %v\n", r)
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var req JsonRpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] Invalid JSON: %v | Input: %s\n", err, line)
			continue
		}

		// Handle notifications (no ID)
		if req.ID == nil {
			if req.Method == "notifications/initialized" {
				fmt.Fprintf(os.Stderr, "[INFO] Client initialized successfully.\n")
			}
			continue
		}

		var result interface{}

		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "go-git-mcp", "version": "0.2.0"},
			}
		case "tools/list":
			// Tool schema structure identical to Rust version
			toolsJSON := `[
				{
					"name": "get_tags",
					"description": "Call this tool BEFORE writing any dependency in Cargo.toml/package.json. Returns the latest versions. Use 'limit: 5' to avoid fetching old tags.",
					"inputSchema": { "type": "object", "properties": { "url": { "type": "string" }, "limit": { "type": "integer" } }, "required": ["url"] }
				},
				{
					"name": "get_changelog",
					"description": "Analyze commit messages between versions.",
					"inputSchema": { "type": "object", "properties": { "url": { "type": "string" }, "start_tag": { "type": "string" }, "end_tag": { "type": "string" } }, "required": ["url", "start_tag", "end_tag"] }
				},
				{
					"name": "get_readme",
					"description": "Read the README to find installation instructions.",
					"inputSchema": { "type": "object", "properties": { "url": { "type": "string" } }, "required": ["url"] }
				},
				{
					"name": "get_file_tree",
					"description": "Explore the repository structure.",
					"inputSchema": { "type": "object", "properties": { "url": { "type": "string" }, "branch": { "type": "string" } }, "required": ["url"] }
				},
				{
					"name": "get_file_content",
					"description": "Read content of source files.",
					"inputSchema": { "type": "object", "properties": { "url": { "type": "string" }, "path": { "type": "string" }, "branch": { "type": "string" } }, "required": ["url", "path"] }
				},
				{
					"name": "search_repository",
					"description": "Search for specific code, structs, functions, or text across the repository. Returns file paths AND code snippets where matches are found.",
					"inputSchema": {
						"type": "object",
						"properties": {
							"url": { "type": "string", "description": "GitHub repository URL" },
							"query": { "type": "string", "description": "Code or text to search (e.g., 'fn main', 'class User', 'dlopen')" }
						},
						"required": ["url", "query"]
					}
				}
			]`
			var tools []map[string]interface{}
			json.Unmarshal([]byte(toolsJSON), &tools)
			result = map[string]interface{}{"tools": tools}

		case "tools/call":
			var params CallParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				result = map[string]interface{}{"isError": true, "content": []map[string]interface{}{{"type": "text", "text": "Invalid params"}}}
				break
			}

			args := params.Arguments
			urlStr := getString(args, "url")
			var data interface{}
			var err error

			switch params.Name {
			case "get_tags":
				limit := 0.0
				if l, ok := args["limit"].(float64); ok {
					limit = l
				}
				data, err = getTags(urlStr, limit)
			case "get_changelog":
				data, err = getChangelog(urlStr, getString(args, "start_tag"), getString(args, "end_tag"))
			case "get_readme":
				data, err = getReadme(urlStr)
			case "get_file_tree":
				data, err = getFileTree(urlStr, getString(args, "branch"))
			case "get_file_content":
				data, err = getFileContent(urlStr, getString(args, "path"), getString(args, "branch"))
			case "search_repository":
				data, err = searchRepository(urlStr, getString(args, "query"))
			default:
				err = fmt.Errorf("Tool '%s' not found", params.Name)
			}

			if err != nil {
				result = map[string]interface{}{
					"isError": true,
					"content": []map[string]interface{}{{"type": "text", "text": err.Error()}},
				}
			} else {
				jsonBytes, _ := json.Marshal(data)
				result = map[string]interface{}{
					"content": []map[string]interface{}{{"type": "text", "text": string(jsonBytes)}},
				}
			}

		default:
			result = map[string]interface{}{}
		}

		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  result,
		}

		respBytes, _ := json.Marshal(response)
		fmt.Println(string(respBytes))
	}
}
