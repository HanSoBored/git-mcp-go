package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"git-mcp/internal/client"
	"git-mcp/internal/tools"
	"git-mcp/internal/types"
	"git-mcp/pkg/argutil"
	"git-mcp/pkg/mcp"
	"git-mcp/pkg/version"
)

func handleInitialize() map[string]interface{} {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]interface{}{"name": "go-git-mcp", "version": version.Get()},
	}
}

func handleToolsList() map[string]interface{} {
	toolsList := make([]map[string]interface{}, len(tools.AllTools))
	for i, tool := range tools.AllTools {
		toolsList[i] = map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		}
	}
	return map[string]interface{}{"tools": toolsList}
}

// handleToolCall executes a tool with the given arguments and formats the response.
//
// Parameters:
//   - ctx: context for cancellation
//   - client: GitHub API client
//   - name: tool name to call
//   - args: tool arguments as a map
//
// Returns an MCP-formatted tool result or error response.
func handleToolCall(ctx context.Context, client *client.GithubClient, name string, args map[string]interface{}) map[string]interface{} {
	data, err := dispatchTool(ctx, client, name, args)
	if err != nil {
		return mcp.ToolErrorText(err.Error())
	}
	jsonBytes, marshalErr := json.Marshal(data)
	if marshalErr != nil {
		return mcp.ToolErrorText(fmt.Sprintf("failed to marshal response: %v", marshalErr))
	}
	return mcp.ToolResultText(string(jsonBytes))
}

// dispatchTool routes a tool call to the appropriate handler function.
//
// Parameters:
//   - ctx: context for cancellation
//   - client: GitHub API client
//   - name: tool name to dispatch
//   - args: tool arguments as a map
//
// Returns the tool result data or an error if the tool is not found.
func dispatchTool(ctx context.Context, client *client.GithubClient, name string, args map[string]interface{}) (interface{}, error) {
	urlStr, _ := argutil.GetString(args, "url")
	switch name {
	case "get_tags":
		return callGetTags(ctx, client, urlStr, args)
	case "get_changelog":
		return callGetChangelog(ctx, client, urlStr, args)
	case "get_readme":
		return callGetReadme(ctx, client, urlStr)
	case "get_file_tree":
		return callGetFileTree(ctx, client, urlStr, args)
	case "get_file_content":
		return callGetFileContent(ctx, client, urlStr, args)
	case "search_repository":
		return callSearchRepository(ctx, client, urlStr, args)
	case "list_issues":
		return callListIssues(ctx, client, urlStr, args)
	case "list_pull_requests":
		return callListPullRequests(ctx, client, urlStr, args)
	case "search_multiple_repos":
		return callSearchMultipleRepos(ctx, client, args)
	case "github_global_search":
		return callGithubGlobalSearch(ctx, client, args)
	default:
		return nil, fmt.Errorf("tool %q not found", name)
	}
}

func callGetTags(ctx context.Context, client *client.GithubClient, urlStr string, args map[string]interface{}) (interface{}, error) {
	limit := argutil.GetInt(args, "limit", 5)
	return tools.GetTags(ctx, client, urlStr, float64(limit))
}

func callGetChangelog(ctx context.Context, client *client.GithubClient, urlStr string, args map[string]interface{}) (interface{}, error) {
	startTag, _ := argutil.GetString(args, "start_tag")
	endTag, _ := argutil.GetString(args, "end_tag")
	return tools.GetChangelog(ctx, client, urlStr, startTag, endTag)
}

func callGetReadme(ctx context.Context, client *client.GithubClient, urlStr string) (interface{}, error) {
	return tools.GetReadme(ctx, client, urlStr)
}

func callGetFileTree(ctx context.Context, client *client.GithubClient, urlStr string, args map[string]interface{}) (interface{}, error) {
	branch, _ := argutil.GetString(args, "branch")
	return tools.GetFileTree(ctx, client, urlStr, branch)
}

func callGetFileContent(ctx context.Context, client *client.GithubClient, urlStr string, args map[string]interface{}) (interface{}, error) {
	path, _ := argutil.GetString(args, "path")
	branch, _ := argutil.GetString(args, "branch")
	return tools.GetFileContent(ctx, client, urlStr, path, branch)
}

func callSearchRepository(ctx context.Context, client *client.GithubClient, urlStr string, args map[string]interface{}) (interface{}, error) {
	query, _ := argutil.GetString(args, "query")
	language, _ := argutil.GetString(args, "language")
	filename, _ := argutil.GetString(args, "filename")
	author, _ := argutil.GetString(args, "author")
	date, _ := argutil.GetString(args, "date")
	limit := argutil.GetInt(args, "limit", 10)
	return tools.SearchRepository(ctx, client, urlStr, query, language, filename, author, date, limit)
}

func callListIssues(ctx context.Context, client *client.GithubClient, urlStr string, args map[string]interface{}) (interface{}, error) {
	state, _ := argutil.GetString(args, "state")
	labels, _ := argutil.GetString(args, "labels")
	author, _ := argutil.GetString(args, "author")
	limit := argutil.GetInt(args, "limit", 30)
	return tools.ListIssues(ctx, client, urlStr, state, labels, author, limit)
}

func callListPullRequests(ctx context.Context, client *client.GithubClient, urlStr string, args map[string]interface{}) (interface{}, error) {
	state, _ := argutil.GetString(args, "state")
	head, _ := argutil.GetString(args, "head")
	base, _ := argutil.GetString(args, "base")
	author, _ := argutil.GetString(args, "author")
	limit := argutil.GetInt(args, "limit", 30)
	return tools.ListPullRequests(ctx, client, urlStr, state, head, base, author, limit)
}

func callSearchMultipleRepos(ctx context.Context, client *client.GithubClient, args map[string]interface{}) (interface{}, error) {
	repos, err := argutil.GetStringArray(args, "repos")
	if err != nil {
		return nil, fmt.Errorf("repos must be an array of strings")
	}
	query, _ := argutil.GetString(args, "query")
	language, _ := argutil.GetString(args, "language")
	filename, _ := argutil.GetString(args, "filename")
	limitPerRepo := argutil.GetInt(args, "limit_per_repo", 10)
	return tools.SearchMultipleRepos(ctx, client, repos, query, language, filename, limitPerRepo)
}

func callGithubGlobalSearch(ctx context.Context, client *client.GithubClient, args map[string]interface{}) (interface{}, error) {
	query, _ := argutil.GetString(args, "query")
	limit := argutil.GetInt(args, "limit", 30)
	return tools.GithubGlobalSearch(ctx, client, query, limit)
}

func handleToolsCallRequest(ctx context.Context, client *client.GithubClient, params json.RawMessage) interface{} {
	var p types.CallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return mcp.ToolErrorText("Invalid params: " + err.Error())
	}
	return handleToolCall(ctx, client, p.Name, p.Arguments)
}

// HandleRequest processes an incoming JSON-RPC request and returns a response.
//
// Parameters:
//   - ctx: context for cancellation
//   - client: GitHub API client
//   - req: JSON-RPC request with method and params
//
// Returns a JSON-RPC response map with result or error.
func HandleRequest(ctx context.Context, client *client.GithubClient, req *types.JsonRpcRequest) map[string]interface{} {
	if req.Method == "" {
		return mcp.JSONRPCError(req.ID, -32600, "Invalid Request")
	}
	result := dispatch(ctx, client, req.Method, req.Params)
	if result == nil {
		return mcp.JSONRPCError(req.ID, -32601, fmt.Sprintf("Method %q not found", req.Method))
	}
	return mcp.SuccessResponse(req.ID, result)
}

func dispatch(ctx context.Context, client *client.GithubClient, method string, params json.RawMessage) interface{} {
	switch method {
	case "initialize":
		return handleInitialize()
	case "tools/list":
		return handleToolsList()
	case "tools/call":
		return handleToolsCallRequest(ctx, client, params)
	default:
		return nil
	}
}
