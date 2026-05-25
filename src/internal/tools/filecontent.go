package tools

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"git-mcp/internal/client"
	"git-mcp/pkg/utils"
)

// GetFileContent reads the content of a specific file
func GetFileContent(
	ctx context.Context,
	client *client.GithubClient,
	link, filePath, branch string,
) (interface{}, error) {
	utils.Debugf("Reading file: %s @ %s", filePath, link)

	// Check required parameters
	if filePath == "" {
		return nil, fmt.Errorf("missing required parameter: path")
	}

	owner, repo, err := utils.ResolveRepo(link)
	if err != nil {
		return nil, err
	}

	if branch == "" {
		branch = "HEAD"
	}

	if err := utils.ValidateBranch(branch); err != nil {
		return nil, err
	}
	cleanPath := strings.TrimPrefix(filePath, "/")

	params := url.Values{}
	params.Set("ref", branch)
	endpoint := fmt.Sprintf("contents/%s?%s", cleanPath, params.Encode())
	body, err := client.DoAPIRaw(ctx, owner, repo, endpoint, "application/vnd.github.raw")
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	content := string(body)

	result := utils.BuildTruncatedResponse(map[string]interface{}{
		"repository": link,
		"path":       cleanPath,
		"ref":        branch,
	}, content, utils.MaxFileContentLength, "content")

	return result, nil
}
