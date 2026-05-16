package tools

import (
	"context"
	"fmt"
	"net/url"

	"git-mcp/internal/client"
	"git-mcp/pkg/types"
	"git-mcp/pkg/utils"
)

// validateListParams validates common list parameters (state, author, limit).
//
// Parameters:
//   - state: filter by state (open, closed, all)
//   - author: filter by author username
//   - limit: maximum results to return
//
// Returns an error if any parameter is invalid. This is a shared helper for
// list operations (issues, pull requests, etc.).
func validateListParams(state, author string, limit int) error {
	if err := utils.ValidateState(state); err != nil {
		return err
	}

	if len(author) > utils.MaxAuthorFilterLen {
		return fmt.Errorf("author filter exceeds maximum length of %d characters", utils.MaxAuthorFilterLen)
	}

	if err := utils.ValidateListLimit(limit); err != nil {
		return err
	}

	return nil
}

// validateIssueParams validates all input parameters for ListIssues
func validateIssueParams(state, labels, author string, limit int) error {
	if err := validateListParams(state, author, limit); err != nil {
		return err
	}

	if len(labels) > utils.MaxLabelsFilterLen {
		return fmt.Errorf("labels filter exceeds maximum length of %d characters", utils.MaxLabelsFilterLen)
	}

	return nil
}

// convertAPIIssuesToDomain converts API response issues to domain model.
//
// Parameters:
//   - apiIssues: slice of IssueAPIResponse from GitHub API
//
// Returns a slice of GitHubIssue domain objects ready for use in the application.
func convertAPIIssuesToDomain(apiIssues []types.IssueAPIResponse) []types.GitHubIssue {
	issues := make([]types.GitHubIssue, len(apiIssues))
	for i, apiIssue := range apiIssues {
		labels := make([]string, len(apiIssue.Labels))
		for j, label := range apiIssue.Labels {
			labels[j] = label.Name
		}
		author := ""
		if apiIssue.User != nil {
			author = apiIssue.User.Login
		}
		issues[i] = types.GitHubIssue{
			Number:    apiIssue.Number,
			Title:     apiIssue.Title,
			State:     apiIssue.State,
			Author:    author,
			CreatedAt: apiIssue.CreatedAt,
			UpdatedAt: apiIssue.UpdatedAt,
			Labels:    labels,
			URL:       apiIssue.HtmlUrl,
		}
	}
	return issues
}

func mapIssues(issues []types.GitHubIssue) []map[string]interface{} {
	result := make([]map[string]interface{}, len(issues))
	for i, issue := range issues {
		result[i] = map[string]interface{}{
			"number":     issue.Number,
			"title":      issue.Title,
			"state":      issue.State,
			"author":     issue.Author,
			"created_at": issue.CreatedAt,
			"updated_at": issue.UpdatedAt,
			"labels":     issue.Labels,
			"url":        issue.URL,
		}
	}
	return result
}

// buildIssueQueryParams builds URL query parameters for issues API.
func buildIssueQueryParams(state, labels, author string, limit int) url.Values {
	params := map[string]string{
		"per_page": fmt.Sprintf("%d", limit),
		"state":    state,
		"labels":   labels,
		"creator":  author,
	}
	return buildQueryParams(params)
}

// ListIssues retrieves issues from a repository with optional filters.
//
// Parameters:
//   - ctx: context for cancellation
//   - client: GitHub API client
//   - link: repository URL
//   - state: filter by state (open, closed, all)
//   - labels: filter by labels (comma-separated)
//   - author: filter by author username
//   - limit: maximum issues to return (1-100)
//
// Returns a list of issues with number, title, state, author, and labels.
func ListIssues(
	ctx context.Context,
	client *client.GithubClient,
	link, state, labels, author string,
	limit int,
) (interface{}, error) {
	utils.Debugf(
		"Fetching issues for: %s (state: %s, labels: %s, author: %s, limit: %d)",
		link, state, labels, author, limit,
	)

	if err := validateIssueParams(state, labels, author, limit); err != nil {
		return nil, err
	}

	owner, repo, err := utils.ResolveRepo(link)
	if err != nil {
		return nil, err
	}

	params := buildIssueQueryParams(state, labels, author, limit)
	endpoint := "issues?" + params.Encode()

	var apiIssues []types.IssueAPIResponse

	if err := client.DoAPI(ctx, owner, repo, endpoint, "", &apiIssues); err != nil {
		return nil, err
	}

	issues := convertAPIIssuesToDomain(apiIssues)
	mappedIssues := mapIssues(issues)

	return map[string]interface{}{
		"repository": link,
		"state":      state,
		"labels":     labels,
		"author":     author,
		"count":      len(mappedIssues),
		"issues":     mappedIssues,
	}, nil
}
