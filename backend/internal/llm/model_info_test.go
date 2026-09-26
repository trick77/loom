package llm

import (
	"testing"

	"github.com/trick77/llmwire"
)

// The context window is the profile's, never a loom constant: a model swap
// must move the UI's context-% denominator with it.
func TestContextWindowComesFromTheChatModelProfile(t *testing.T) {
	p, err := llmwire.Default().Lookup(textModel)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", textModel, err)
	}
	if p.Limits.Context <= 0 {
		t.Fatalf("profile %q declares no context window", textModel)
	}
	if got := ContextWindow(); got != p.Limits.Context {
		t.Fatalf("ContextWindow() = %d, want the profile's %d", got, p.Limits.Context)
	}
}
