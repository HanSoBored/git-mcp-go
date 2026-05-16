package utils

import "time"

// ============================================================================
// Constants - Magic numbers replaced with named constants
// ============================================================================
const (
	MaxScannerBufferSize     = 10 * 1024 * 1024 // Maximum scanner buffer size (10MB)
	MaxReadmeLength          = 20000            // Maximum README content length before truncation
	MaxFileContentLength     = 30000            // Maximum file content length before truncation
	MaxFileTreeEntries       = 1000             // Maximum file tree entries before truncation
	MaxChangelogEntries      = 200              // Maximum changelog entries before truncation
	GithubAPITimeout         = 30 * time.Second // Timeout for GitHub API requests
	GithubAPI                = "https://api.github.com"
	MaxLanguageFilterLen     = 20
	MaxFilenameFilterLen     = 100
	MaxAuthorFilterLen       = 39 // GitHub username max length
	MaxDateFilterLen         = 30 // e.g., "2024-01-01..2024-12-31"
	MaxSearchQueryLen        = 512
	MaxListLimit             = 100
	DefaultListLimit         = 30
	MaxLabelsFilterLen       = 256
	MaxBranchFilterLen       = 100
	MaxReposPerSearch        = 10
	DefaultLimitPerRepo      = 10
	MaxGlobalSearchLimit     = 100
	DefaultGlobalSearchLimit = 30
)
