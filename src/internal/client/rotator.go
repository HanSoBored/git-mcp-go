package client

import (
	"log/slog"
	"strings"
	"sync"
)

// TokenRotator provides round-robin rotation of GitHub API tokens.
// It is safe for concurrent use.
type TokenRotator struct {
	mu          sync.Mutex
	tokens      []string // immutable after NewTokenRotator; safe to read without mu
	current     int
	rotations   int
}

// knownTokenPrefixes lists all currently valid GitHub token prefixes.
// See: https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens
var knownTokenPrefixes = []string{
	"ghp_",
	"github_pat_",
	"gho_",
	"ghs_",
	"ghr_",
	"ghu_",
}

// isValidGitHubToken performs basic validation of a GitHub token format.
func isValidGitHubToken(token string) bool {
	for _, prefix := range knownTokenPrefixes {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

// NewTokenRotator creates a new TokenRotator with the given tokens.
// Empty tokens are filtered out. Tokens that fail format validation are logged as warnings.
func NewTokenRotator(tokens []string) *TokenRotator {
	filtered := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t != "" {
			if !isValidGitHubToken(t) {
				slog.Warn("token does not match known GitHub token format", "prefix", t[:min(len(t), 8)])
			}
			filtered = append(filtered, t)
		}
	}
	return &TokenRotator{
		tokens:  filtered,
		current: 0,
	}
}

// Current returns the currently active token, or "" if no tokens are configured.
func (r *TokenRotator) Current() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Len() == 0 {
		return ""
	}
	return r.tokens[r.current]
}

// Rotate advances to the next token in the rotation using modular arithmetic.
// Returns:
//   - token: the newly active token ("", false if no tokens)
//   - exhausted: true if rotation wrapped back to starting index (all tokens used)
//   - ok: false if no tokens are configured
func (r *TokenRotator) Rotate() (token string, exhausted bool, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.tokens) == 0 {
		return "", false, false
	}
	r.current = (r.current + 1) % len(r.tokens)
	r.rotations++
	// Report exhausted when we have wrapped through all distinct tokens.
	// For single-token rotators, exhausted is true on every rotation.
	exhausted = r.rotations >= len(r.tokens)
	if exhausted {
		r.rotations = 0
	}
	return r.tokens[r.current], exhausted, true
}

// Len returns the number of configured tokens.
// Safe for concurrent use because tokens is immutable after construction.
func (r *TokenRotator) Len() int {
	return len(r.tokens)
}
