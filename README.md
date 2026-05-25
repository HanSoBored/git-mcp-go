# Git MCP Go

[![Built with Go](https://img.shields.io/badge/built_with-Go-00ADD8.svg)](https://go.dev/)
[![MCP](https://img.shields.io/badge/protocol-MCP-blue)](https://modelcontextprotocol.io/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

---

**Git Remote MCP** is a high-performance Model Context Protocol (MCP) server written in Go. It empowers AI Agents (Claude Desktop, Gemini CLI, Cursor, Qwen) to explore, search, and analyze GitHub repositories in **real-time** without the need for local cloning.

### The Problem It Solves
AI models often suffer from a "knowledge cutoff" or hallucinate library versions. This tool provides **Real-Time Context**:
- **Outdated Dependencies:** AI can fetch the absolute latest SemVer-sorted tags to recommend up-to-date libraries.
- **Context Gap:** AI can "read" remote source code, file structures, and documentation to understand libraries it wasn't trained on.
- **Blind Coding:** Instead of guessing APIs, the AI can search and read the actual implementation in the repository.

| Gemini CLI in Action | Qwen CLI in Action |
|:--------------------:|:------------------:|
| ![Gemini CLI Preview](previews/gemini.jpg) | ![Qwen CLI Preview](previews/qwen.jpg) |

---

## Why Go?

1. **Zero System Dependencies:** No OpenSSL or C-bindings required.
2. **Super Fast Compilation:** Rebuilds complete in under 0.5 seconds.
3. **Easy Cross-Compilation:** Build for any platform with simple `GOOS/GOARCH` environment variables.
4. **Concurrency Ready:** Future features can leverage Go's `goroutines` for parallel repository operations.

---

## Features

- **Zero-Clone Exploration:** Fetch trees, files, and metadata via GitHub API instantly.
- **Smart Dependency Solving**: Fetches and sorts tags by **Semantic Versioning (SemVer)**, ensuring the AI suggests the *actual* latest version.
- **Semantic Search:** Find functions, structs, or text across the entire repository using GitHub's Search API.
- **Token Rotation**: Supports **multiple** `GITHUB_TOKEN_1..32` variables for automatic fallback when a token is rate-limited. Also reads `GITHUB_TOKEN` and `GH_TOKEN`.
- **Intelligent Rate Limiting**: Automatically detects primary (token-based) vs secondary (abuse) rate limits, rotates tokens on primary limits, and backs off with `Retry-After` on secondary limits.
- **Secure & Scalable**: Natively supports `GITHUB_TOKEN` authentication to increase API rate limits from 60 to **5,000 requests/hour**.
- **Multi-Arch Support**: Cross-compile for `x86_64`, `aarch64` (ARM64), and `armv7` easily.

---

## Tools Available for AI

| Tool | Description |
|------|-------------|
| `get_tags` | Returns latest tags/versions. Supports `limit` and **SemVer sorting** (e.g., `v1.10` > `v1.9`). |
| `search_repository` | Search for code, functions, or text. Optional filters: `language`, `filename`. Note: `author` and `date` filters are **not supported** by the GitHub Code Search API and will be ignored (logged as warning). |
| `github_global_search` | Search across **ALL of GitHub** (global search). Filters: `language`, `filename`. Max results: 100 (default: 30). |
| `get_file_tree` | Recursively lists files to reveal project architecture/structure. |
| `get_file_content` | Reads the raw content of specific files from any branch/tag. |
| `get_readme` | Automatically fetches the default README for a quick project overview. |
| `get_changelog` | Compares two tags and returns a summary of commit messages. |
| `list_issues` | List issues with filtering by `state`, `labels`, `author`. Default limit: 30. |
| `list_pull_requests` | List PRs with filtering by `state`, `head`, `base`, `author`. Default limit: 30. |
| `search_multiple_repos` | Search across up to 10 repos. Filters: `language`, `filename`. Results per repo: 10 (default). |

---

## Visualizing the Data Flow

```mermaid
sequenceDiagram
    participant User
    participant AI as AI Agent
    participant MCP as Git-Remote-MCP
    participant GitHub as GitHub API

    User->>AI: "Use library X in my project"
    AI->>MCP: get_tags(url_X, limit=5)
    MCP->>GitHub: Request Tags Metadata (Auth Token)
    GitHub-->>MCP: Raw Tags Data
    Note over MCP: Sorting via SemVer Logic
    MCP-->>AI: ["v2.1.0", "v2.0.9", ...]

    AI->>MCP: search_repository(query="struct Config")
    MCP->>GitHub: Search Code API
    GitHub-->>MCP: Result Paths
    MCP-->>AI: Found in "src/config.rs"

    AI->>User: "Here is the code using v2.1.0 and correct config struct"
```

---

## Installation

### Option A: Quick Install (Binary)

**Recommended (Secure):** Download and review the script first, then execute:

```bash
# Download the install script
curl -fsSL https://raw.githubusercontent.com/HanSoBored/git-mcp-go/main/installation/install.sh -o install.sh

# Review the script (important!)
cat install.sh

# Execute if you trust it
bash install.sh
rm install.sh
```

**Use at your own risk (One-liner):**
```bash
curl -fsSL https://raw.githubusercontent.com/HanSoBored/git-mcp-go/main/installation/install.sh | bash
```

### Option B: Build from Source (Go)

Requires Go 1.21+:

```bash
git clone https://github.com/HanSoBored/git-mcp-go.git
cd git-mcp-go
./installation/build.sh
```

### Option C: Manual Build

```bash
cd src/
go mod download
go build -ldflags="-s -w" -o git_mcp

# Cross-compile for Linux x86_64
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o git_mcp_linux_amd64

# Cross-compile for macOS ARM64
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o git_mcp_darwin_arm64
```

---

## Configuration

Add this to your MCP client configuration (Claude Desktop, Cursor, Gemini CLI, etc.):

```json
{
  "mcpServers": {
    "git-remote": {
      "command": "/usr/local/bin/git_mcp",
      "args": [],
      "env": {
        "GITHUB_TOKEN": "ghp_your_personal_access_token_here"
      }
    }
  }
}
```

**Important:** Adding a `GITHUB_TOKEN` is highly recommended to avoid the 60 requests/hour rate limit. With authentication, you get **5,000 requests/hour**.

### Token Rotation (Optional)

For higher reliability under heavy usage, configure multiple tokens. When a token hits its primary rate limit, the server automatically rotates to the next available token:

```json
{
  "mcpServers": {
    "git-remote": {
      "command": "/usr/local/bin/git_mcp",
      "args": [],
      "env": {
        "GITHUB_TOKEN_1": "ghp_token_one",
        "GITHUB_TOKEN_2": "ghp_token_two",
        "GITHUB_TOKEN_3": "ghp_token_three"
      }
    }
  }
}
```

The server reads tokens from (in priority order): `GITHUB_TOKEN`, `GH_TOKEN`, and `GITHUB_TOKEN_1` through `GITHUB_TOKEN_32`. Duplicate tokens are automatically deduplicated. Primary rate limits trigger a token rotation; secondary rate limits (abuse detection) trigger a timed backoff instead.

---

## System Prompt for AI Agents

To maximize the utility of this MCP, add this to your Agent's system instructions:

```text
You are equipped with the 'git-remote' MCP toolset.
1. When asked about a library/dependency, ALWAYS use 'get_tags' with 'limit: 5' to verify the latest version. Do not guess.
2. Before suggesting implementation details, use 'search_repository' to find relevant code definitions (structs, functions).
3. Use 'get_file_content' to read the actual code context before answering.
4. If a user asks about a project structure, use 'get_file_tree'.
5. For issue/PR tracking, use 'list_issues' and 'list_pull_requests' with appropriate filters (state, labels, author).
6. To search across multiple repositories, use 'search_multiple_repos' (max 10 repos per call).
7. For cross-project patterns, use 'github_global_search' to search ALL of GitHub (not limited to specific repos).
8. Leverage search filters (`language`, `filename`) to narrow down 'search_repository' results. Note: `author` and `date` are **not supported** by the GitHub Code Search API, only by commit search.
```

---

## Example Usage

### Check Latest Version
```
User: "What's the latest version of clap?"
AI: [Calls get_tags with url="https://github.com/clap-rs/clap", limit=5]
AI: "The latest version is v4.5.59"
```

### Search for Code
```
User: "How does this project handle dlopen?"
AI: [Calls search_repository with query="dlopen"]
AI: [Calls get_file_content to read the relevant file]
AI: "Here's how dlopen is handled..."
```

### Search with Filters
```
User: "Find all TypeScript files with 'config' in the filename"
AI: [Calls search_repository with query="config", filename="*.ts", language="TypeScript"]
AI: "Found 5 TypeScript config files..."
```

### List Issues
```
User: "Show open issues labeled 'bug' by author 'john'"
AI: [Calls list_issues with url="https://github.com/org/repo", state="open", labels="bug", author="john", limit=20]
AI: "Found 3 matching issues..."
```

### List Pull Requests
```
User: "Show PRs targeting main branch from feature/* branches"
AI: [Calls list_pull_requests with url="https://github.com/org/repo", base="main", head="feature/", state="open"]
AI: "Found 2 open PRs..."
```

### Multi-Repo Search
```
User: "Search for 'auth middleware' across our microservices"
AI: [Calls search_multiple_repos with repos=[url1, url2, url3], query="auth middleware", language="Go", limit_per_repo=5]
AI: "Results from 3 repos: service-a (2), service-b (1), service-c (0)..."
```

### Global GitHub Search
```
User: "Find all Go implementations of rate limiting across GitHub"
AI: [Calls github_global_search with query="rate limiter", language="Go", limit=50]
AI: "Found 50 repositories with rate limiting implementations..."
```

---

## License

MIT License. Feel free to use and contribute!
