package tools

import (
	"fmt"
	"net/url"

	"git-mcp/pkg/types"
)

// buildSearchQuery constructs a GitHub code search query with filters.
//
// Parameters:
//   - query: the search query string
//   - owner, repo: repository owner and name
//   - language, filename, author, date: optional filters
//
// Returns a formatted search query string for the GitHub Code Search API.
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

// mapTextMatches extracts snippets from TextMatches.
func mapTextMatches(textMatches []types.TextMatch) []string {
	snippets := make([]string, 0, len(textMatches))
	for _, match := range textMatches {
		snippets = append(snippets, match.Fragment)
	}
	return snippets
}

func mapSearchResults(items []types.SearchItem) []map[string]interface{} {
	result := make([]map[string]interface{}, len(items))
	for i, item := range items {
		result[i] = map[string]interface{}{
			"file":     item.Name,
			"path":     item.Path,
			"url":      item.HtmlUrl,
			"snippets": mapTextMatches(item.TextMatches),
		}
	}
	return result
}

type searchResult struct {
	repo   string
	result interface{}
	err    error
}

// buildQueryParams constructs url.Values from a map, skipping empty values.
//
// Parameters:
//   - params: map of parameter names to values (empty values are skipped)
//
// Returns url.Values with non-empty parameters encoded.
func buildQueryParams(params map[string]string) url.Values {
	result := url.Values{}
	for key, value := range params {
		if value != "" {
			result.Add(key, value)
		}
	}
	return result
}
