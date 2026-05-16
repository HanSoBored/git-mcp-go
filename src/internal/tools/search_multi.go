package tools

import (
	"context"
	"fmt"
	"sync"

	"git-mcp/internal/client"
	"git-mcp/pkg/utils"
)

// searchJob represents a single repository search task.
type searchJob struct {
	repoURL  string
	query    string
	language string
	filename string
	limit    int
}

// validateParams validates parameters for SearchMultipleRepos.
func validateParams(repos []string, query string) error {
	if err := utils.ValidateQuery(query); err != nil {
		return err
	}

	if len(repos) == 0 {
		return fmt.Errorf("repos array cannot be empty")
	}

	if len(repos) > utils.MaxReposPerSearch {
		return fmt.Errorf("too many repositories: %d (max: %d)", len(repos), utils.MaxReposPerSearch)
	}

	for i, repo := range repos {
		if repo == "" {
			return fmt.Errorf("repository URL at index %d is empty", i)
		}
		if _, _, err := utils.ParseGitHubURL(repo); err != nil {
			return fmt.Errorf("invalid repository URL at index %d: %w", i, err)
		}
	}

	return nil
}

// deduplicateRepos removes duplicate repository URLs from a list.
func deduplicateRepos(repos []string) []string {
	seen := make(map[string]bool)
	uniqueRepos := []string{}
	for _, repo := range repos {
		if !seen[repo] {
			seen[repo] = true
			uniqueRepos = append(uniqueRepos, repo)
		}
	}
	return uniqueRepos
}

// runConcurrentSearch executes search jobs concurrently with semaphore limiting.
//
// Parameters:
//   - ctx: context for cancellation
//   - client: GitHub API client
//   - jobs: slice of searchJob to execute
//
// Returns a channel of searchResult that will be closed when all jobs complete.
func runConcurrentSearch(ctx context.Context, client *client.GithubClient, jobs []searchJob) <-chan searchResult {
	resultChan := make(chan searchResult, len(jobs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)

	for _, job := range jobs {
		wg.Add(1)
		go func(job searchJob) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			result, err := SearchRepository(ctx, client, job.repoURL, job.query, job.language, job.filename, "", "", job.limit)
			select {
			case resultChan <- searchResult{repo: job.repoURL, result: result, err: err}:
			case <-ctx.Done():
				return
			}
		}(job)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	return resultChan
}

// aggregatedData holds intermediate results from concurrent repository searches.
type aggregatedData struct {
	results     []map[string]interface{}
	errors      []string
	repoResults map[string]interface{}
}

// collectResults aggregates results from concurrent search operations.
//
// Parameters:
//   - resultsChan: channel receiving search results from concurrent operations
//   - repos: list of repository URLs being searched
//
// Returns aggregatedData containing all results, errors, and per-repo status.
func collectResults(resultsChan <-chan searchResult, repos []string) *aggregatedData {
	data := &aggregatedData{
		results:     make([]map[string]interface{}, 0),
		errors:      make([]string, 0),
		repoResults: make(map[string]interface{}),
	}

	for res := range resultsChan {
		if res.err != nil {
			data.errors = append(data.errors, fmt.Sprintf("%s: %v", res.repo, res.err))
			data.repoResults[res.repo] = map[string]interface{}{
				"success": false,
				"error":   res.err.Error(),
			}
			continue
		}

		resultData, ok := res.result.(map[string]interface{})
		if !ok {
			data.errors = append(data.errors, fmt.Sprintf("%s: unexpected result type", res.repo))
			data.repoResults[res.repo] = map[string]interface{}{
				"success": false,
				"error":   "unexpected result type",
			}
			continue
		}

		results, ok := resultData["results"].([]map[string]interface{})
		if !ok {
			data.errors = append(data.errors, fmt.Sprintf("%s: unexpected results type", res.repo))
			data.repoResults[res.repo] = map[string]interface{}{
				"success": false,
				"error":   "unexpected results type",
			}
			continue
		}

		for _, r := range results {
			r["repository"] = res.repo
			data.results = append(data.results, r)
		}

		data.repoResults[res.repo] = map[string]interface{}{
			"success":     true,
			"total_found": resultData["total_found"],
			"count":       len(results),
		}
	}

	return data
}

// buildMultiRepoResponse formats aggregated search data for MCP response.
//
// Parameters:
//   - data: aggregated search results from collectResults
//   - repos: list of repository URLs that were searched
//   - query: the search query string
//
// Returns a map containing the formatted response for the MCP client.
func buildMultiRepoResponse(data *aggregatedData, repos []string, query string) map[string]interface{} {
	response := map[string]interface{}{
		"query":          query,
		"repos_searched": len(repos),
		"repos":          repos,
		"total_results":  len(data.results),
		"results":        data.results,
		"repo_status":    data.repoResults,
	}

	if len(data.errors) > 0 {
		response["errors"] = data.errors
	}

	return response
}

// aggregateResults combines results from concurrent repository searches.
func aggregateResults(resultsChan <-chan searchResult, repos []string, query string) (interface{}, error) {
	data := collectResults(resultsChan, repos)
	return buildMultiRepoResponse(data, repos, query), nil
}

// SearchMultipleRepos searches for code across multiple repositories concurrently.
//
// Parameters:
//   - ctx: context for cancellation
//   - client: GitHub API client
//   - repos: list of repository URLs to search
//   - query: search query string
//   - language, filename: optional filters
//   - limitPerRepo: maximum results per repository
//
// Returns aggregated results from all repositories with per-repo status.
func SearchMultipleRepos(
	ctx context.Context,
	client *client.GithubClient,
	repos []string,
	query, language, filename string,
	limitPerRepo int,
) (interface{}, error) {
	if err := validateParams(repos, query); err != nil {
		return nil, err
	}

	uniqueRepos := deduplicateRepos(repos)

	if limitPerRepo < 1 {
		limitPerRepo = utils.DefaultLimitPerRepo
	}

	jobs := make([]searchJob, len(uniqueRepos))
	for i, repo := range uniqueRepos {
		jobs[i] = searchJob{
			repoURL:  repo,
			query:    query,
			language: language,
			filename: filename,
			limit:    limitPerRepo,
		}
	}

	resultsChan := runConcurrentSearch(ctx, client, jobs)

	return aggregateResults(resultsChan, uniqueRepos, query)
}
