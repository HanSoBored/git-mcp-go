package tools

import (
	"context"
	"fmt"
	"net/url"

	"git-mcp/internal/client"
	"git-mcp/pkg/types"
	"git-mcp/pkg/utils"
)

// validatePullRequestParams validates all input parameters for ListPullRequests
func validatePullRequestParams(state, head, base, author string, limit int) error {
	if err := validateListParams(state, author, limit); err != nil {
		return err
	}

	if err := utils.ValidateBranchFilter(head, "head"); err != nil {
		return err
	}

	if err := utils.ValidateBranchFilter(base, "base"); err != nil {
		return err
	}

	return nil
}

// convertAPIPRsToDomain converts API response pull requests to domain model.
//
// Parameters:
//   - apiPRs: slice of PullRequestAPIResponse from GitHub API
//
// Returns a slice of GitHubPullRequest domain objects ready for use.
func convertAPIPRsToDomain(apiPRs []types.PullRequestAPIResponse) []types.GitHubPullRequest {
	prs := make([]types.GitHubPullRequest, len(apiPRs))
	for i, apiPR := range apiPRs {
		var mergeable interface{}
		if apiPR.Mergeable != nil {
			mergeable = *apiPR.Mergeable
		} else {
			mergeable = nil
		}
		author := ""
		if apiPR.User != nil {
			author = apiPR.User.Login
		}
		prs[i] = types.GitHubPullRequest{
			Number:     apiPR.Number,
			Title:      apiPR.Title,
			State:      apiPR.State,
			Author:     author,
			CreatedAt:  apiPR.CreatedAt,
			UpdatedAt:  apiPR.UpdatedAt,
			HeadBranch: apiPR.Head.Ref,
			BaseBranch: apiPR.Base.Ref,
			Mergeable:  mergeable,
			Merged:     apiPR.Merged,
			URL:        apiPR.HtmlUrl,
		}
	}
	return prs
}

func mapPullRequests(prs []types.GitHubPullRequest) []map[string]interface{} {
	result := make([]map[string]interface{}, len(prs))
	for i, pr := range prs {
		result[i] = map[string]interface{}{
			"number":      pr.Number,
			"title":       pr.Title,
			"state":       pr.State,
			"author":      pr.Author,
			"created_at":  pr.CreatedAt,
			"updated_at":  pr.UpdatedAt,
			"head_branch": pr.HeadBranch,
			"base_branch": pr.BaseBranch,
			"mergeable":   pr.Mergeable,
			"merged":      pr.Merged,
			"url":         pr.URL,
		}
	}
	return result
}

// buildPullRequestQueryParams builds URL query parameters for pull requests API.
func buildPullRequestQueryParams(state, head, base, author string, limit int) url.Values {
	params := map[string]string{
		"per_page": fmt.Sprintf("%d", limit),
		"state":    state,
		"head":     head,
		"base":     base,
		"creator":  author,
	}
	return buildQueryParams(params)
}

// ListPullRequests retrieves pull requests from a repository with optional filters.
//
// Parameters:
//   - ctx: context for cancellation
//   - client: GitHub API client
//   - link: repository URL
//   - state: filter by state (open, closed, all)
//   - head: filter by head branch (user:branch)
//   - base: filter by base branch
//   - author: filter by author username
//   - limit: maximum PRs to return (1-100)
//
// Returns a list of pull requests with number, title, state, branches, and merge status.
func ListPullRequests(
	ctx context.Context,
	client *client.GithubClient,
	link, state, head, base, author string,
	limit int,
) (interface{}, error) {
	utils.Debugf(
		"Fetching pull requests for: %s (state: %s, head: %s, base: %s, author: %s, limit: %d)",
		link, state, head, base, author, limit,
	)

	if err := validatePullRequestParams(state, head, base, author, limit); err != nil {
		return nil, err
	}

	owner, repo, err := utils.ResolveRepo(link)
	if err != nil {
		return nil, err
	}

	params := buildPullRequestQueryParams(state, head, base, author, limit)
	endpoint := "pulls?" + params.Encode()

	var apiPRs []types.PullRequestAPIResponse

	if err := client.DoAPI(ctx, owner, repo, endpoint, "", &apiPRs); err != nil {
		return nil, err
	}

	prs := convertAPIPRsToDomain(apiPRs)
	mappedPRs := mapPullRequests(prs)

	return map[string]interface{}{
		"repository":    link,
		"state":         state,
		"head":          head,
		"base":          base,
		"author":        author,
		"count":         len(mappedPRs),
		"pull_requests": mappedPRs,
	}, nil
}
