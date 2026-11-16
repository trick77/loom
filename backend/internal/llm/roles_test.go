package llm

import (
	"strings"
	"testing"

	"github.com/trick77/toolkit/llmwire/llmwiretest"
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

// Image turns stream with the full tool list, so the vision model must have
// tool calling as well as image input.
func TestResolveRoles_VisionModelNeedsTools(t *testing.T) {
	_, err := ResolveRoles(llmwiretest.Registry(), Roles{Chat: llmwiretest.ChatModel, Vision: llmwiretest.BudgetModel})
	if err == nil || !strings.Contains(err.Error(), "tools") {
		t.Fatalf("error = %v, want one naming the missing tools", err)
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
	// Chat is vision too here: one answering model, listed once.
	if len(info.Answering) != 1 || info.Answering[0].Model != llmwiretest.ChatModel || info.Answering[0].DisplayName != p.DisplayName {
		t.Fatalf("answering = %+v, want the chat model once", info.Answering)
	}
}

// Every distinct model an answer can come from is named, so an answer's
// recorded model always finds its name.
func TestResolved_InfoListsTheAnsweringModels(t *testing.T) {
	resolved, err := ResolveRoles(llmwiretest.Registry(), Roles{Chat: llmwiretest.ChatModel, Deep: llmwiretest.MessagesModel})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range resolved.Info().Answering {
		got = append(got, m.Model)
	}
	if len(got) != 2 || got[0] != llmwiretest.ChatModel || got[1] != llmwiretest.MessagesModel {
		t.Fatalf("answering = %v, want chat then deep, each once", got)
	}
}
