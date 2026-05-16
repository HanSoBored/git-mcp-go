package tools

import (
	"context"
	"fmt"
	"net/url"

	"git-mcp/internal/client"
	"git-mcp/pkg/types"
	"git-mcp/pkg/utils"
)

// searchParams holds parsed search parameters.
type searchParams struct {
	owner    string
	repo     string
	query    string
	language string
	filename string
	author   string
	date     string
	limit    int
}

// validateSearchParams validates search parameters for SearchRepository.
func validateSearchParams(query, language, filename, author, date string, limit int) error {
	if err := utils.ValidateQuery(query); err != nil {
		return err
	}

	if err := utils.ValidateSearchFilters(language, filename, author, date); err != nil {
		return err
	}

	if err := utils.ValidateListLimit(limit); err != nil {
		return err
	}

	return nil
}

// buildSearchEndpoint builds the search API endpoint URL
func buildSearchEndpoint(owner, repo, query, language, filename, author, date string, limit int) string {
	q := buildSearchQuery(query, owner, repo, language, filename, author, date)

	params := url.Values{}
	params.Add("q", q)
	params.Add("per_page", fmt.Sprintf("%d", limit))

	return fmt.Sprintf("%s/search/code?%s", utils.GithubAPI, params.Encode())
}

// parseSearchParams extracts and validates search parameters from inputs.
func parseSearchParams(link, query, language, filename, author, date string, limit int) (*searchParams, error) {
	// Try to extract from search URL first
	owner, repo, extractedQuery, err := utils.ParseGitHubSearchURL(link)
	if err == nil && owner != "" && repo != "" {
		// Valid search URL with repo: qualifier
		if extractedQuery != "" && query == "" {
			query = extractedQuery
		}
	} else {
		// Not a search URL or parsing failed, resolve as regular repo URL
		owner, repo, err = utils.ResolveRepo(link)
		if err != nil {
			return nil, err
		}
	}

	if err := validateSearchParams(query, language, filename, author, date, limit); err != nil {
		return nil, err
	}

	return &searchParams{
		owner:    owner,
		repo:     repo,
		query:    query,
		language: language,
		filename: filename,
		author:   author,
		date:     date,
		limit:    limit,
	}, nil
}

// executeSearch performs the HTTP call to GitHub code search API.
func executeSearch(
	ctx context.Context,
	client *client.GithubClient,
	params *searchParams,
) (*types.SearchAPIResponse, error) {
	apiUrl := buildSearchEndpoint(
		params.owner,
		params.repo,
		params.query,
		params.language,
		params.filename,
		params.author,
		params.date,
		params.limit,
	)

	resp, err := client.Get(ctx, apiUrl, "application/vnd.github.v3.text-match+json")
	if err != nil {
		return nil, err
	}

	if err := utils.HandleAPIError(resp); err != nil {
		return nil, err
	}

	var result types.SearchAPIResponse
	if err := client.DecodeJSON(resp, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// formatSearchResults formats API response for MCP.
func formatSearchResults(link, query string, result *types.SearchAPIResponse) map[string]interface{} {
	results := mapSearchResults(result.Items)

	return map[string]interface{}{
		"repository":  link,
		"query":       query,
		"total_found": result.TotalCount,
		"results":     results,
	}
}

// SearchRepository searches for code in repositories.
//
// Parameters:
//   - ctx: context for cancellation
//   - client: GitHub API client
//   - link: repository URL or search URL
//   - query, language, filename, author, date: search filters
//   - limit: maximum results to return
//
// Returns a map with repository info, query, total count, and results.
func SearchRepository(
	ctx context.Context,
	client *client.GithubClient,
	link, query, language, filename, author, date string,
	limit int,
) (interface{}, error) {
	utils.Debugf("Searching '%s' in %s", query, link)

	params, err := parseSearchParams(link, query, language, filename, author, date, limit)
	if err != nil {
		return nil, err
	}

	result, err := executeSearch(ctx, client, params)
	if err != nil {
		return nil, err
	}

	return formatSearchResults(link, query, result), nil
}
