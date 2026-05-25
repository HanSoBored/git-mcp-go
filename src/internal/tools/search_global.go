package tools

import (
	"context"
	"fmt"

	"git-mcp/internal/client"
	"git-mcp/pkg/types"
	"git-mcp/pkg/utils"
)

// validateGlobalSearchParams validates the search query and limit parameters.
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

	apiURL := utils.SearchCodeURL(query, limit)

	var result types.GlobalSearchAPIResponse
	if err := client.DoRaw(ctx, apiURL, acceptTextMatch, &result); err != nil {
		return nil, err
	}

	return formatGlobalSearchResults(&result, query, limit), nil
}

// formatGlobalSearchResults converts a GlobalSearchAPIResponse into an MCP-friendly map.
func formatGlobalSearchResults(result *types.GlobalSearchAPIResponse, query string, limit int) map[string]interface{} {
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
	}
}
