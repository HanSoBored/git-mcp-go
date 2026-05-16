package tools

import (
	"context"
	"fmt"
	"strings"

	"git-mcp/internal/client"
	"git-mcp/pkg/utils"
)

// formatCommits formats commit entries for changelog output
func formatCommits(commits []struct {
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}) []string {
	result := make([]string, len(commits))
	for i, c := range commits {
		msg := strings.Split(c.Commit.Message, "\n")[0]
		date := strings.Split(c.Commit.Author.Date, "T")[0]
		result[i] = fmt.Sprintf("[%s] %s", date, msg)
	}
	return result
}

// GetChangelog fetches commit messages between two tags
func GetChangelog(ctx context.Context, client *client.GithubClient, link, v1, v2 string) (interface{}, error) {
	utils.Debugf("Fetching changelog: %s...%s", v1, v2)

	// Check required parameters
	if v1 == "" {
		return nil, fmt.Errorf("missing required parameter: start_tag")
	}
	if v2 == "" {
		return nil, fmt.Errorf("missing required parameter: end_tag")
	}

	owner, repo, err := utils.ResolveRepo(link)
	if err != nil {
		return nil, err
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

	if err := client.DoAPI(ctx, owner, repo, fmt.Sprintf("compare/%s...%s", v1, v2), "", &result); err != nil {
		return nil, err
	}

	summaries := formatCommits(result.Commits)

	content := strings.Join(summaries, "\n")
	response := utils.BuildTruncatedResponse(map[string]interface{}{
		"repository": link,
		"from":       v1,
		"to":         v2,
	}, content, utils.MaxChangelogEntries*50, "changes")

	return response, nil
}
