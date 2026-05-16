package tools

import "git-mcp/internal/types"

// AllTools defines all available tools as Go structs (no JSON string round-trip)
var AllTools = []types.ToolDef{
	{
		Name:        "get_tags",
		Description: "Call this tool BEFORE writing any dependency in Cargo.toml/package.json. " +
			"Returns the latest versions. Use 'limit: 5' to avoid fetching old tags.",
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
		Description: "Search for code, structs, functions, or text in a repository. " +
			"Returns file paths and code snippets where matches are found.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":      map[string]interface{}{"type": "string", "description": "GitHub repository URL"},
				"query":    map[string]interface{}{"type": "string", "description": "Code or text to search (e.g., 'fn main', 'class User', 'dlopen')"},
				"language": map[string]interface{}{"type": "string", "description": "Filter by programming language (e.g., 'Go', 'Python')"},
				"filename": map[string]interface{}{"type": "string", "description": "Filter by filename or path pattern"},
				"author":   map[string]interface{}{"type": "string", "description": "Filter by commit author username"},
				"date":     map[string]interface{}{"type": "string", "description": "Filter by commit date (e.g., '>=2024-01-01', '<=2024-12-31')"},
				"limit":    map[string]interface{}{"type": "integer", "description": "Maximum results (1-100, default: 10)"},
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
		Description: "Search for code across multiple GitHub repositories in parallel. " +
			"Returns aggregated results from all repositories.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"repos": map[string]interface{}{
					"type":        "array",
					"items":       map[string]string{"type": "string"},
					"description": "List of GitHub repository URLs (max 10)",
				},
				"query":          map[string]interface{}{"type": "string", "description": "Code or text to search"},
				"language":       map[string]interface{}{"type": "string", "description": "Filter by programming language"},
				"filename":       map[string]interface{}{"type": "string", "description": "Filter by filename pattern"},
				"limit_per_repo": map[string]interface{}{"type": "integer", "description": "Max results per repo (default: 10)"},
			},
			"required": []string{"repos", "query"},
		},
	},
	{
		Name:        "github_global_search",
		Description: "Search code across ALL of GitHub (global search). " +
			"Use for finding patterns across multiple projects.",
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
