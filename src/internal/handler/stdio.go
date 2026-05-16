package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"git-mcp/internal/client"
	"git-mcp/internal/types"
	"git-mcp/pkg/mcp"
	"git-mcp/pkg/utils"
)

// writeResponse writes a JSON-RPC response to stdout
func writeResponse(response map[string]interface{}) {
	respBytes, err := json.Marshal(response)
	if err != nil {
		slog.Error("failed to marshal response", "error", err)
		return
	}
	fmt.Fprintf(os.Stdout, "%s\n", string(respBytes))
}

// handleNotification processes notifications (requests without an ID).
func handleNotification(req types.JsonRpcRequest) {
	switch req.Method {
	case "notifications/initialized":
		slog.Info("Client initialized")
	default:
		slog.Debug("Unknown notification", "method", req.Method)
	}
}

// ServeStdio handles the main stdio loop for JSON-RPC communication
func ServeStdio(client *client.GithubClient) {
	scanner := bufio.NewScanner(os.Stdin)
	// Set max input line size to maxScannerBufferSize to prevent resource exhaustion
	scanner.Buffer(make([]byte, 0), utils.MaxScannerBufferSize)

	ctx := context.Background()

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var req types.JsonRpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			slog.Error("Invalid JSON", "error", err)
			writeResponse(mcp.JSONRPCError(nil, -32700, "Parse error"))
			continue
		}

		if req.ID == nil {
			handleNotification(req)
			continue
		}

		writeResponse(HandleRequest(ctx, client, &req))
	}

	if err := scanner.Err(); err != nil {
		slog.Error("stdin read error", "error", err)
	}
}
