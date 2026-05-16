package tools

import (
	"context"
	"fmt"
	"net/url"

	"git-mcp/internal/client"
	"git-mcp/pkg/types"
	"git-mcp/pkg/utils"
)

// validateGlobalSearchParams validates parameters for GithubGlobalSearch.
func validateGlobalSearchParams(query string, limit int) error {
	if err := utils.ValidateQuery(query); err != nil {
		return err
	}

	if limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}

	if limit > utils.MaxGlobalSearchLimit {
		return fmt.Errorf("limit exceeds maximum of %d", utils.MaxGlobalSearchLimit)
	}

	return nil
}

// buildGlobalSearchEndpoint builds the global search API endpoint URL
func buildGlobalSearchEndpoint(query string, limit int) string {
	if limit <= 0 {
		limit = utils.DefaultGlobalSearchLimit
	}

	params := url.Values{}
	params.Add("q", query)
	params.Add("per_page", fmt.Sprintf("%d", limit))

	return fmt.Sprintf("%s/search/code?%s", utils.GithubAPI, params.Encode())
}

// GithubGlobalSearch performs a code search across all of GitHub.
//
// Parameters:
//   - ctx: context for cancellation
//   - client: GitHub API client
//   - query: search query string
//   - limit: maximum results to return (default: 30, max: 100)
//
// Returns search results with repository information for each match.
func GithubGlobalSearch(
	ctx context.Context,
	client *client.GithubClient,
	query string,
	limit int,
) (interface{}, error) {
	utils.Debugf("Global search for: '%s' (limit: %d)", query, limit)

	if err := validateGlobalSearchParams(query, limit); err != nil {
		return nil, err
	}

	apiUrl := buildGlobalSearchEndpoint(query, limit)

	resp, err := client.Get(ctx, apiUrl, "application/vnd.github.v3.text-match+json")
	if err != nil {
		return nil, err
	}

	if err := utils.HandleAPIError(resp); err != nil {
		return nil, err
	}

	var result types.GlobalSearchAPIResponse

	if err := client.DecodeJSON(resp, &result); err != nil {
		return nil, err
	}

	results := make([]map[string]interface{}, len(result.Items))
	for i, item := range result.Items {
		results[i] = map[string]interface{}{
			"file":       item.Name,
			"path":       item.Path,
			"url":        item.HtmlUrl,
			"repository": item.Repository.FullName,
			"snippets":   mapTextMatches(item.TextMatches),
		}
	}

	return map[string]interface{}{
		"query":       query,
		"total_found": result.TotalCount,
		"limit":       limit,
		"results":     results,
	}, nil
}
