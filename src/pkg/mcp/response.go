package mcp

// ToolResultText creates a CallToolResult with success content
func ToolResultText(content string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": content}},
	}
}

// ToolErrorText creates a CallToolResult with an error message
func ToolErrorText(message string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": message}},
		"isError": true,
	}
}

// JSONRPCError creates a JSON-RPC protocol error response
func JSONRPCError(id any, code int64, message string) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
}

// SuccessResponse creates a standard JSON-RPC success response
func SuccessResponse(id any, result interface{}) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
}
