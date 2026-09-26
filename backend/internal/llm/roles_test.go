package llm

import (
	"strings"
	"testing"

	"github.com/trick77/llmwire/llmwiretest"
)

func TestResolveRoles_GateAndVisionDefaultToChat(t *testing.T) {
	resolved, err := ResolveRoles(llmwiretest.Registry(), Roles{Chat: llmwiretest.ChatModel})
	if err != nil {
		t.Fatalf("ResolveRoles: %v", err)
	}
	want := Roles{Chat: llmwiretest.ChatModel, Gate: llmwiretest.ChatModel, Vision: llmwiretest.ChatModel}
	if resolved.Roles != want {
		t.Fatalf("roles = %+v, want %+v", resolved.Roles, want)
	}
}

// A model short of what its role needs, or an id llmwire does not know, fails
// at boot with the ids that would work.
func TestResolveRoles_RefusesWithTheValidChoices(t *testing.T) {
	for _, tc := range []struct {
		name  string
		roles Roles
	}{
		{"unknown chat model", Roles{Chat: "no-such-model"}},
		{"chat model without tools", Roles{Chat: llmwiretest.BudgetModel}},
		{"vision model without vision", Roles{Chat: llmwiretest.ChatModel, Vision: llmwiretest.BudgetModel}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveRoles(llmwiretest.Registry(), tc.roles)
			if err == nil {
				t.Fatal("ResolveRoles accepted it")
			}
			if !strings.Contains(err.Error(), llmwiretest.ChatModel) {
				t.Fatalf("error %q does not name a valid choice", err)
			}
		})
	}
}

func TestResolveRoles_NoChatModelIsAnError(t *testing.T) {
	if _, err := ResolveRoles(llmwiretest.Registry(), Roles{}); err == nil {
		t.Fatal("ResolveRoles accepted an empty chat model")
	}
}

// The facts loom shows or checks come from the chat model's profile.
func TestResolved_InfoAndKeysComeFromTheProfiles(t *testing.T) {
	resolved, err := ResolveRoles(llmwiretest.Registry(), Roles{Chat: llmwiretest.ChatModel})
	if err != nil {
		t.Fatalf("ResolveRoles: %v", err)
	}
	info := resolved.Info()
	p, _ := llmwiretest.Registry().Lookup(llmwiretest.ChatModel)
	if info.ID != llmwiretest.ChatModel || info.DisplayName != p.DisplayName || info.ContextWindow != p.Limits.Context {
		t.Fatalf("info = %+v, want the chat profile's facts", info)
	}
	if keys := resolved.KeyEnvs(); len(keys) != 1 || keys[0] != p.APIKeyEnv() {
		t.Fatalf("key envs = %v, want [%s] once for three roles on one provider", keys, p.APIKeyEnv())
	}
}
