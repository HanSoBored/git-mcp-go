package tools

import (
	"context"
	"fmt"
	"strings"

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
	utils.Debugf("Fetching Tree: %s", link)

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

	if err := client.DoAPI(ctx, owner, repo, fmt.Sprintf("git/trees/%s?recursive=1", branch), "", &result); err != nil {
		return nil, err
	}

	files := flattenTree(result.Tree)

	// Truncate if more than maxFileTreeEntries
	fileListStr := strings.Join(files, "\n")
	truncateResult := utils.Truncate(fileListStr, utils.MaxFileTreeEntries*50)

	response := map[string]interface{}{
		"repository": link,
		"ref":        branch,
		"files":      files,
	}

	// Update files with truncated content
	if truncateResult.IsTruncated {
		lines := strings.Split(truncateResult.Content, "\n")
		response["files"] = lines
		response["is_truncated"] = true
		response["original_length"] = truncateResult.OriginalLength
		response["truncated_at"] = truncateResult.TruncatedAt
	}

	return response, nil
}
