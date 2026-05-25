package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"

	"git-mcp/internal/client"
	"git-mcp/internal/handler"
	"git-mcp/pkg/version"
)

// buildVersion is injected at build time via -ldflags="-X main.buildVersion=1.0.1"
var buildVersion string

func loadGitHubTokens() []string {
	seen := make(map[string]bool)
	var tokens []string

	addIfUnique := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			tokens = append(tokens, t)
		}
	}

	addIfUnique(os.Getenv("GITHUB_TOKEN"))
	addIfUnique(os.Getenv("GH_TOKEN"))

	// Scans GITHUB_TOKEN_1 through GITHUB_TOKEN_N for token rotation.
	// Gaps in numbering are skipped (e.g., if token_3 is unset, iteration continues).
	const maxEnvTokens = 32
	for i := 1; i <= maxEnvTokens; i++ {
		addIfUnique(os.Getenv(fmt.Sprintf("GITHUB_TOKEN_%d", i)))
	}

	return tokens
}

func main() {
	if printVersion() {
		return
	}
	setupLogging()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("FATAL CRASH", "error", r, "stack", string(debug.Stack()))
			os.Exit(1)
		}
	}()
	gc := createClient()
	handler.ServeStdio(gc)
}

func printVersion() bool {
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()
	if !*versionFlag {
		return false
	}
	v := buildVersion
	if v == "" {
		v = "dev"
	}
	fmt.Printf("git-mcp version %s\n", v)
	return true
}

func setupLogging() {
	logLevel := slog.LevelInfo
	if os.Getenv("GIT_MCP_DEBUG") == "1" {
		logLevel = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})))
}

func createClient() *client.GithubClient {
	version.Set(buildVersion)
	tokens := loadGitHubTokens()
	if len(tokens) == 0 {
		slog.Info("No GITHUB_TOKEN set; using unauthenticated requests (60 req/hr)")
	} else {
		slog.Info("GitHub tokens loaded", "count", len(tokens))
	}
	return client.NewGithubClient(tokens...)
}
