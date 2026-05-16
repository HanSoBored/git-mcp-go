package types

// GitHubIssue represents a GitHub issue from the API
type GitHubIssue struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Author    string   `json:"author"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Labels    []string `json:"labels"`
	URL       string   `json:"url"`
}

// GitHubPullRequest represents a GitHub pull request from the API
type GitHubPullRequest struct {
	Number     int         `json:"number"`
	Title      string      `json:"title"`
	State      string      `json:"state"`
	Author     string      `json:"author"`
	CreatedAt  string      `json:"created_at"`
	UpdatedAt  string      `json:"updated_at"`
	HeadBranch string      `json:"head_branch"`
	BaseBranch string      `json:"base_branch"`
	Mergeable  interface{} `json:"mergeable"`
	Merged     bool        `json:"merged"`
	URL        string      `json:"url"`
}

// GitHubSearchResult represents a code search result
type GitHubSearchResult struct {
	File     string   `json:"file"`
	Path     string   `json:"path"`
	URL      string   `json:"url"`
	Snippets []string `json:"snippets"`
}

// GitHubGlobalSearchResult represents a global search result with repository info
type GitHubGlobalSearchResult struct {
	File       string   `json:"file"`
	Path       string   `json:"path"`
	URL        string   `json:"url"`
	Repository string   `json:"repository"`
	Snippets   []string `json:"snippets"`
}
