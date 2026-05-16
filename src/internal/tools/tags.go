package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"git-mcp/internal/client"
	"git-mcp/pkg/utils"
)

// GetTags fetches Git tags from a repository and sorts them by SemVer
func GetTags(ctx context.Context, client *client.GithubClient, link string, limit float64) (interface{}, error) {
	utils.Debugf("Fetching tags for: %s (limit: %v)", link, limit)

	owner, repo, err := utils.ResolveRepo(link)
	if err != nil {
		return nil, err
	}

	// Add 30s timeout context to prevent resource exhaustion
	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Reconstruct safe URL from validated owner/repo to prevent command injection
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

	lines := strings.Split(string(out), "\n")
	var tags []string
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 {
			tag := strings.TrimPrefix(parts[1], "refs/tags/")
			tags = append(tags, tag)
		}
	}

	// Sort tags using Semantic Versioning (descending)
	utils.SemverDesc(tags)

	if limit > 0 && int(limit) < len(tags) {
		tags = tags[:int(limit)]
	}

	return map[string]interface{}{
		"repository":    link,
		"count":         len(tags),
		"limit_applied": limit,
		"tags":          tags,
	}, nil
}
