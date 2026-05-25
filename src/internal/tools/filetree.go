package tools

import (
	"context"
	"fmt"
	"net/url"

	"git-mcp/internal/client"
	"git-mcp/pkg/utils"
)

// flattenTree extracts file paths from a tree entry list
func flattenTree(tree []struct {
	Path string `json:"path"`
	Type string `json:"type"`
}) []string {
	result := make([]string, len(tree))
	for i, entry := range tree {
		if entry.Type == "tree" {
			result[i] = entry.Path + "/"
		} else {
			result[i] = entry.Path
		}
	}
	return result
}

// GetFileTree fetches the file tree structure of a repository
func GetFileTree(ctx context.Context, client *client.GithubClient, link, branch string) (interface{}, error) {
	utils.Debugf("Fetching tree: %s", link)

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

	var result struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}

	params := url.Values{}
	params.Set("recursive", "1")
	endpoint := fmt.Sprintf("git/trees/%s?%s", branch, params.Encode())
	if err := client.DoAPI(ctx, owner, repo, endpoint, "", &result); err != nil {
		return nil, err
	}

	files := flattenTree(result.Tree)
	return buildTreeResponse(link, branch, files), nil
}

func buildTreeResponse(link, branch string, files []string) map[string]interface{} {
	response := map[string]interface{}{
		"repository": link,
		"ref":        branch,
		"files":      files,
	}

	if len(files) > utils.MaxFileTreeEntries {
		response["files"] = files[:utils.MaxFileTreeEntries]
		response["is_truncated"] = true
		response["original_length"] = len(files)
		response["truncated_at"] = utils.MaxFileTreeEntries
	}

	return response
}
