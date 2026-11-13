package main

import (
	"testing"

	"github.com/trick77/loom/internal/config"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/toolkit/llmwire/llmwiretest"
)

func TestChatModelInfo_EmptyWhenChatIsOff(t *testing.T) {
	resolved, err := llm.ResolveRoles(llmwiretest.Registry(), llm.Roles{Chat: llmwiretest.ChatModel})
	if err != nil {
		t.Fatal(err)
	}
	if got := chatModelInfo(config.Config{ChatModels: resolved, ChatEnabled: false}); got.ID != "" || len(got.Slots) != 0 {
		t.Fatalf("chat off: %+v, want nothing", got)
	}
	if got := chatModelInfo(config.Config{ChatModels: resolved, ChatEnabled: true}); got.ID != llmwiretest.ChatModel {
		t.Fatalf("chat on: %+v, want the chat model", got)
	}
}
