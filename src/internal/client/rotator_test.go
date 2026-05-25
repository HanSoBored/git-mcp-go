package client

import (
	"sync"
	"testing"
)

func TestTokenRotator_Empty(t *testing.T) {
	// nil tokens
	r := NewTokenRotator(nil)
	if r.Len() != 0 {
		t.Errorf("expected 0 tokens, got %d", r.Len())
	}
	if tok := r.Current(); tok != "" {
		t.Errorf("expected empty token, got %q", tok)
	}
	_, _, ok := r.Rotate()
	if ok {
		t.Error("Rotate on empty rotator should return ok=false")
	}

	// empty tokens
	r = NewTokenRotator([]string{})
	if r.Len() != 0 {
		t.Errorf("expected 0 tokens, got %d", r.Len())
	}
	if tok := r.Current(); tok != "" {
		t.Errorf("expected empty token, got %q", tok)
	}
	_, _, ok = r.Rotate()
	if ok {
		t.Error("Rotate on empty rotator should return ok=false")
	}

	// all-empty strings are filtered out
	r = NewTokenRotator([]string{"", "  ", ""})
	if r.Len() != 0 {
		t.Errorf("expected 0 tokens after filtering whitespace, got %d", r.Len())
	}
}

func TestTokenRotator_RoundRobin(t *testing.T) {
	tokens := []string{"token-a", "token-b", "token-c"}
	r := NewTokenRotator(tokens)

	type step struct {
		name        string
		action      func() (string, bool, bool)
		wantToken   string
		wantExh     bool
		wantOK      bool
		wantCurrent string
	}

	steps := []step{
		{name: "initial current", action: func() (string, bool, bool) { return r.Current(), false, r.Len() > 0 }, wantToken: "token-a", wantOK: true, wantCurrent: "token-a"},
		{name: "first rotation", action: r.Rotate, wantToken: "token-b", wantExh: false, wantOK: true, wantCurrent: "token-b"},
		{name: "second rotation", action: r.Rotate, wantToken: "token-c", wantExh: false, wantOK: true, wantCurrent: "token-c"},
		{name: "wrap exhausted", action: r.Rotate, wantToken: "token-a", wantExh: true, wantOK: true, wantCurrent: "token-a"},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			tok, exh, ok := step.action()
			if tok != step.wantToken {
				t.Errorf("token = %q, want %q", tok, step.wantToken)
			}
			if exh != step.wantExh {
				t.Errorf("exhausted = %v, want %v", exh, step.wantExh)
			}
			if ok != step.wantOK {
				t.Errorf("ok = %v, want %v", ok, step.wantOK)
			}
			if cur := r.Current(); cur != step.wantCurrent {
				t.Errorf("Current() = %q, want %q", cur, step.wantCurrent)
			}
		})
	}
}

func TestTokenRotator_ConcurrentSafety(t *testing.T) {
	tokens := []string{"t1", "t2", "t3", "t4"}
	r := NewTokenRotator(tokens)

	var wg sync.WaitGroup
	const goroutines = 20
	const opsPerGoroutine = 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				_ = r.Len()
				_ = r.Current()
				r.Rotate()
			}
		}()
	}

	wg.Wait()

	// After concurrent access, the rotator should still be usable
	if r.Len() != 4 {
		t.Errorf("expected 4 tokens after concurrent access, got %d", r.Len())
	}

	// Verify round-robin still works correctly
	tok1, _, ok := r.Rotate()
	if !ok {
		t.Fatal("Rotate should return ok=true")
	}
	tok2, _, _ := r.Rotate()
	tok3, _, _ := r.Rotate()
	tok4, _, _ := r.Rotate()

	// With 4 tokens, rotating 4 times should yield all different tokens
	seen := map[string]bool{tok1: true, tok2: true, tok3: true, tok4: true}
	if len(seen) != 4 {
		t.Errorf("expected 4 unique tokens after full rotation cycle, got %d: %v", len(seen), seen)
	}
}
