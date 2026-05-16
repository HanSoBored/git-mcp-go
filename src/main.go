package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"

	"git-mcp/internal/client"
	"git-mcp/internal/handler"
	"git-mcp/pkg/version"
)

// buildVersion is injected at build time via -ldflags="-X main.buildVersion=1.0.1"
var buildVersion string

func main() {
	// Add --version flag support
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag {
		v := buildVersion
		if v == "" {
			v = "dev"
		}
		fmt.Printf("git-mcp version %s\n", v)
		os.Exit(0)
	}

	// Initialize structured logging
	logLevel := slog.LevelInfo
	if os.Getenv("GIT_MCP_DEBUG") == "1" {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))

	// Catch panic (similar to std::panic::set_hook in Rust)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("FATAL CRASH", "error", r, "stack", string(debug.Stack()))
			os.Exit(1)
		}
	}()

	// Set version before creating any clients that may read it
	version.Set(buildVersion)

	// Create GitHub API client with token from environment
	token := os.Getenv("GITHUB_TOKEN")
	client := client.NewGithubClient(token)

	// Log auth status once at startup
	if token != "" {
		slog.Info("Using GITHUB_TOKEN for authentication")
	} else {
		slog.Info("No GITHUB_TOKEN, using unauthenticated requests")
	}

	// Start stdio server loop
	handler.ServeStdio(client)
}
