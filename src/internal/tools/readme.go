package tools

import (
	"context"

	"git-mcp/internal/client"
	"git-mcp/pkg/utils"
)

// GetReadme fetches the README file from a GitHub repository
func GetReadme(ctx context.Context, client *client.GithubClient, link string) (interface{}, error) {
	utils.Debugf("Fetching README: %s", link)

	owner, repo, err := utils.ResolveRepo(link)
	if err != nil {
		return nil, err
	}

	body, err := client.DoAPIRaw(ctx, owner, repo, "readme", "application/vnd.github.raw")
	if err != nil {
		return nil, err
	}

	content := string(body)

	result := utils.BuildTruncatedResponse(map[string]interface{}{
		"repository": link,
		"type":       "readme",
	}, content, utils.MaxReadmeLength, "content")

	return result, nil
}
