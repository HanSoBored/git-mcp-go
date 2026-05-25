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
	"git-mcp/pkg/utils"
	"git-mcp/pkg/version"
)

// handleInitialize returns the server capabilities for MCP initialization.
func handleInitialize() map[string]interface{} {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
		"serverInfo":      map[string]interface{}{"name": "git-mcp-go", "version": version.Get()},
	}
}

// handleToolsList returns the list of available MCP tools.
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
//   - gc: GitHub API client
//   - name: tool name to call
//   - args: tool arguments as a map
//
// Returns an MCP-formatted tool result or error response.
func marshalToolResult(data interface{}) map[string]interface{} {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return mcp.ToolErrorText(fmt.Sprintf("failed to marshal response: %v", err))
	}
	return mcp.ToolResultText(string(jsonBytes))
}

func handleToolCall(
	ctx context.Context,
	gc *client.GithubClient,
	name string,
	args map[string]interface{},
) map[string]interface{} {
	data, err := dispatchTool(ctx, gc, name, args)
	if err != nil {
		return mcp.ToolErrorText(err.Error())
	}
	return marshalToolResult(data)
}

// dispatchTool routes a tool call to the appropriate handler function.
//
// Parameters:
//   - ctx: context for cancellation
//   - gc: GitHub API client
//   - name: tool name to dispatch
//   - args: tool arguments as a map
//
// Returns the tool result data or an error if the tool is not found.
func dispatchTool(
	ctx context.Context,
	gc *client.GithubClient,
	name string,
	args map[string]interface{},
) (interface{}, error) {
	switch name {
	case "search_multiple_repos":
		return callSearchMultipleRepos(ctx, gc, args)
	case "github_global_search":
		return callGithubGlobalSearch(ctx, gc, args)
	}

	urlStr, err := argutil.GetRequiredString(args, "url")
	if err != nil {
		return nil, err
	}

	switch name {
	case "get_tags":
		return callGetTags(ctx, gc, urlStr, args)
	case "get_changelog":
		return callGetChangelog(ctx, gc, urlStr, args)
	case "get_readme":
		return callGetReadme(ctx, gc, urlStr)
	case "get_file_tree":
		return callGetFileTree(ctx, gc, urlStr, args)
	case "get_file_content":
		return callGetFileContent(ctx, gc, urlStr, args)
	case "search_repository":
		return callSearchRepository(ctx, gc, urlStr, args)
	case "list_issues":
		return callListIssues(ctx, gc, urlStr, args)
	case "list_pull_requests":
		return callListPullRequests(ctx, gc, urlStr, args)
	default:
		return nil, fmt.Errorf("tool %q not found", name)
	}
}

func callGetTags(
	ctx context.Context,
	gc *client.GithubClient,
	urlStr string,
	args map[string]interface{},
) (interface{}, error) {
	limit := argutil.GetInt(args, "limit", 5)
	return tools.GetTags(ctx, gc, urlStr, limit)
}

func callGetChangelog(
	ctx context.Context,
	gc *client.GithubClient,
	urlStr string,
	args map[string]interface{},
) (interface{}, error) {
	startTag, _ := argutil.GetString(args, "start_tag")
	endTag, _ := argutil.GetString(args, "end_tag")
	return tools.GetChangelog(ctx, gc, urlStr, startTag, endTag)
}

func callGetReadme(
	ctx context.Context,
	gc *client.GithubClient,
	urlStr string,
) (interface{}, error) {
	return tools.GetReadme(ctx, gc, urlStr)
}

func callGetFileTree(
	ctx context.Context,
	gc *client.GithubClient,
	urlStr string,
	args map[string]interface{},
) (interface{}, error) {
	branch, _ := argutil.GetString(args, "branch")
	return tools.GetFileTree(ctx, gc, urlStr, branch)
}

func callGetFileContent(
	ctx context.Context,
	gc *client.GithubClient,
	urlStr string,
	args map[string]interface{},
) (interface{}, error) {
	path, _ := argutil.GetString(args, "path")
	branch, _ := argutil.GetString(args, "branch")
	return tools.GetFileContent(ctx, gc, urlStr, path, branch)
}

func callSearchRepository(
	ctx context.Context,
	gc *client.GithubClient,
	urlStr string,
	args map[string]interface{},
) (interface{}, error) {
	query, _ := argutil.GetString(args, "query")
	language, _ := argutil.GetString(args, "language")
	filename, _ := argutil.GetString(args, "filename")
	author, _ := argutil.GetString(args, "author")
	date, _ := argutil.GetString(args, "date")
	limit := argutil.GetInt(args, "limit", 10)
	return tools.SearchRepository(ctx, gc, urlStr, query, language, filename, author, date, limit)
}

func callListIssues(
	ctx context.Context,
	gc *client.GithubClient,
	urlStr string,
	args map[string]interface{},
) (interface{}, error) {
	state, _ := argutil.GetString(args, "state")
	labels, _ := argutil.GetString(args, "labels")
	author, _ := argutil.GetString(args, "author")
	limit := argutil.GetInt(args, "limit", 30)
	return tools.ListIssues(ctx, gc, urlStr, state, labels, author, limit)
}

func callListPullRequests(
	ctx context.Context,
	gc *client.GithubClient,
	urlStr string,
	args map[string]interface{},
) (interface{}, error) {
	state, _ := argutil.GetString(args, "state")
	head, _ := argutil.GetString(args, "head")
	base, _ := argutil.GetString(args, "base")
	author, _ := argutil.GetString(args, "author")
	limit := argutil.GetInt(args, "limit", 30)
	return tools.ListPullRequests(ctx, gc, urlStr, state, head, base, author, limit)
}

func callSearchMultipleRepos(
	ctx context.Context,
	gc *client.GithubClient,
	args map[string]interface{},
) (interface{}, error) {
	repos, err := argutil.GetStringArray(args, "repos")
	if err != nil {
		return nil, fmt.Errorf("repos must be an array of strings: %w", err)
	}
	query, _ := argutil.GetString(args, "query")
	language, _ := argutil.GetString(args, "language")
	filename, _ := argutil.GetString(args, "filename")
	limitPerRepo := argutil.GetInt(args, "limit_per_repo", 10)
	return tools.SearchMultipleRepos(ctx, gc, repos, query, language, filename, limitPerRepo)
}

func callGithubGlobalSearch(
	ctx context.Context,
	gc *client.GithubClient,
	args map[string]interface{},
) (interface{}, error) {
	query, _ := argutil.GetString(args, "query")
	limit := argutil.GetInt(args, "limit", utils.DefaultGlobalSearchLimit)
	return tools.GithubGlobalSearch(ctx, gc, query, limit)
}

func handleToolsCallRequest(ctx context.Context, gc *client.GithubClient, params json.RawMessage) interface{} {
	var p types.CallParams
	if err := json.Unmarshal(params, &p); err != nil {
		return mcp.ToolErrorText("Invalid params: " + err.Error())
	}
	return handleToolCall(ctx, gc, p.Name, p.Arguments)
}

// HandleRequest processes an incoming JSON-RPC request and returns a response.
//
// Parameters:
//   - ctx: context for cancellation
//   - gc: GitHub API client
//   - req: JSON-RPC request with method and params
//
// Returns a JSON-RPC response map with result or error.
func HandleRequest(ctx context.Context, gc *client.GithubClient, req *types.JsonRpcRequest) map[string]interface{} {
	if req.Method == "" {
		return mcp.JSONRPCError(req.ID, -32600, "Invalid Request")
	}
	result := dispatch(ctx, gc, req.Method, req.Params)
	if result == nil {
		return mcp.JSONRPCError(req.ID, -32601, fmt.Sprintf("Method %q not found", req.Method))
	}
	return mcp.SuccessResponse(req.ID, result)
}

func dispatch(ctx context.Context, gc *client.GithubClient, method string, params json.RawMessage) interface{} {
	switch method {
	case "initialize":
		return handleInitialize()
	case "tools/list":
		return handleToolsList()
	case "tools/call":
		return handleToolsCallRequest(ctx, gc, params)
	default:
		return nil
	}
}
