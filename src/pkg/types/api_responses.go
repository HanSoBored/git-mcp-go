package types

// TextMatch represents a single text match in search results
type TextMatch struct {
	Fragment string `json:"fragment"`
}

// PullRequestAPIResponse represents the GitHub API response for pull requests
type PullRequestAPIResponse struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	User      *struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
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

// IssueAPIResponse represents the GitHub API response for issues
type IssueAPIResponse struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	User      *struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Labels    []struct {
		Name string `json:"name"`
	} `json:"labels"`
	HtmlUrl string `json:"html_url"`
}

// SearchItem represents a code search result item
type SearchItem struct {
	Name        string      `json:"name"`
	Path        string      `json:"path"`
	HtmlUrl     string      `json:"html_url"`
	TextMatches []TextMatch `json:"text_matches"`
}

// SearchAPIResponse represents the GitHub API response for code search
type SearchAPIResponse struct {
	TotalCount int         `json:"total_count"`
	Items      []SearchItem `json:"items"`
}

// GlobalSearchItem represents a global search result item with repository info
type GlobalSearchItem struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"`
	HtmlUrl    string      `json:"html_url"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	TextMatches []TextMatch `json:"text_matches"`
}

// GlobalSearchAPIResponse represents the GitHub API response for global search
type GlobalSearchAPIResponse struct {
	TotalCount int              `json:"total_count"`
	Items      []GlobalSearchItem `json:"items"`
}
