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

// The key variable is the profile provider's, so a model moved to another
// provider moves the enable check and the startup hint with it.
func TestAPIKeyEnvComesFromTheChatModelProfile(t *testing.T) {
	p, err := llmwire.Default().Lookup(textModel)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", textModel, err)
	}
	if got := APIKeyEnv(); got == "" || got != p.APIKeyEnv() {
		t.Fatalf("APIKeyEnv() = %q, want the profile's %q", got, p.APIKeyEnv())
	}
}

// loom picks the levels, the profile says which exist: a level the model does
// not take must fail at init, not as a refusal on every request.
func TestReasoningEffortsAreAcceptedByTheProfile(t *testing.T) {
	for _, level := range []string{turnReasoningEffort, helperReasoningEffort} {
		if err := checkEffort(chatProfile, level); err != nil {
			t.Fatalf("checkEffort(%q): %v", level, err)
		}
	}
	if err := checkEffort(chatProfile, "ludicrous"); err == nil {
		t.Fatal(`checkEffort("ludicrous") = nil, want an error`)
	}
}
