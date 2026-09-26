package rag

import (
	"strings"
	"testing"

	"github.com/trick77/llmwire/llmwiretest"
)

// The width and key variable are the profile's, never restated in loom.
func TestResolveEmbedModel_ReadsTheProfile(t *testing.T) {
	m, err := ResolveEmbedModel(llmwiretest.Registry(), llmwiretest.EmbedModel)
	if err != nil {
		t.Fatalf("ResolveEmbedModel: %v", err)
	}
	p, _ := llmwiretest.Registry().Lookup(llmwiretest.EmbedModel)
	if m.ID != llmwiretest.EmbedModel || m.Width != llmwiretest.EmbedDimensions || m.KeyEnv != p.APIKeyEnv() {
		t.Fatalf("model = %+v, want the profile's id, width %d and key %s", m, llmwiretest.EmbedDimensions, p.APIKeyEnv())
	}
}

// An unknown id, or a chat model named as the embedding model, fails boot
// with the embedding models that would work.
func TestResolveEmbedModel_RefusesWithTheValidChoices(t *testing.T) {
	for _, id := range []string{"no-such-model", llmwiretest.ChatModel} {
		_, err := ResolveEmbedModel(llmwiretest.Registry(), id)
		if err == nil || !strings.Contains(err.Error(), llmwiretest.EmbedModel) {
			t.Fatalf("ResolveEmbedModel(%q) error = %v, want one listing %s", id, err, llmwiretest.EmbedModel)
		}
	}
}
