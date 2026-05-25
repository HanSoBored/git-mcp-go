package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"git-mcp/internal/client"
	"git-mcp/pkg/utils"
)

// GetTags uses git ls-remote directly, not the GitHub API client.
// The _ *client.GithubClient parameter is kept for interface uniformity
// with other tool functions.
func GetTags(ctx context.Context, _ *client.GithubClient, link string, limit int) (interface{}, error) {
	utils.Debugf("Fetching tags for: %s (limit: %v)", link, limit)

	owner, repo, err := utils.ResolveRepo(link)
	if err != nil {
		return nil, err
	}

	tags, err := fetchGitTags(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	return formatTagResponse(link, tags, limit), nil
}

// fetchGitTags runs git ls-remote and parses the tag list from a validated GitHub URL.
func fetchGitTags(ctx context.Context, owner, repo string) ([]string, error) {
	// 30s timeout prevents resource exhaustion on large repositories with many tags
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	safeURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	cmd := exec.CommandContext(cmdCtx, "git", "ls-remote", "--tags", "--refs", "--", safeURL)
	cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("git ls-remote timed out after 30s")
		}
		return nil, fmt.Errorf("git ls-remote error: %w", err)
	}

	var tags []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.IndexByte(line, '\t')
		if idx >= 0 {
			tag := strings.TrimPrefix(line[idx+1:], "refs/tags/")
			if tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	return tags, nil
}

// formatTagResponse sorts tags by SemVer, caps to limit, and builds the response map.
func formatTagResponse(link string, tags []string, limit int) map[string]interface{} {
	sorted := append([]string(nil), tags...)
	utils.SemverDesc(sorted)

	if limit > 0 && limit < len(sorted) {
		sorted = sorted[:limit]
	}

	return map[string]interface{}{
		"repository":    link,
		"count":         len(sorted),
		"limit_applied": limit,
		"tags":          sorted,
	}
}
