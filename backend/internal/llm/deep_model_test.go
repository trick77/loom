package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/trick77/toolkit/llmwire/llmwiretest"
)

// The deep slot is optional and, when set, must take a turn like the chat
// model does: tools and streaming.
func TestResolveRoles_DeepModel(t *testing.T) {
	resolved, err := ResolveRoles(llmwiretest.Registry(), Roles{Chat: llmwiretest.ChatModel, Deep: llmwiretest.MessagesModel})
	if err != nil {
		t.Fatalf("ResolveRoles: %v", err)
	}
	slots := resolved.Slots()
	if len(slots) != 2 || slots[0].Key != SlotStandard || slots[1].Key != SlotDeep {
		t.Fatalf("slots = %+v, want standard then deep", slots)
	}
	if slots[1].Model != llmwiretest.MessagesModel || slots[1].DisplayName == "" || slots[1].ContextWindow == 0 {
		t.Fatalf("deep slot %+v does not describe the deep profile", slots[1])
	}
	if got := resolved.DeepKeyEnv(); got == "" {
		t.Fatal("DeepKeyEnv is empty for a configured deep model")
	}

	_, err = ResolveRoles(llmwiretest.Registry(), Roles{Chat: llmwiretest.ChatModel, Deep: llmwiretest.BudgetModel})
	if err == nil || !strings.Contains(err.Error(), "deep") {
		t.Fatalf("a deep model without tools: err = %v, want one naming the deep role", err)
	}

	plain, err := ResolveRoles(llmwiretest.Registry(), Roles{Chat: llmwiretest.ChatModel})
	if err != nil {
		t.Fatal(err)
	}
	if got := plain.Slots(); len(got) != 1 || got[0].Key != SlotStandard {
		t.Fatalf("no deep model: slots = %+v, want standard only", got)
	}
	if plain.DeepKeyEnv() != "" {
		t.Fatal("no deep model: deep slot reported")
	}
	if got := resolved.WithoutDeep().Slots(); len(got) != 1 {
		t.Fatalf("WithoutDeep: slots = %+v, want standard only", got)
	}
}

// A deep turn runs on the deep model at the deep intent, with the cap as an
// answer budget so the model's thinking allowance comes on top.
func TestClient_DeepTurnRoutesAndAsksDeep(t *testing.T) {
	srv := llmwiretest.NewServer(t)
	client := mustClient(t, Config{BaseURL: srv.URL, Timeout: 5 * time.Second,
		Models: Roles{Chat: testModel, Deep: llmwiretest.MessagesModel}}, nil)
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{Deep: true})

	res, err := client.StreamChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, nil, func(StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("StreamChatWithTools: %v", err)
	}
	last := srv.Last()
	if last.Model() != llmwiretest.MessagesModel || res.Model != llmwiretest.MessagesModel {
		t.Fatalf("model = %q (result %q), want the deep model", last.Model(), res.Model)
	}
	if last.Reasoning() != llmwiretest.DeepSent || res.ReasoningEffort != llmwiretest.DeepSent {
		t.Fatalf("reasoning = %q (result %q), want %q", last.Reasoning(), res.ReasoningEffort, llmwiretest.DeepSent)
	}
	if got, _ := last.MaxTokens(); got != defaultMaxCompletionTokens+llmwiretest.DeepOverhead {
		t.Fatalf("cap = %d, want answer %d + deep overhead %d", got, defaultMaxCompletionTokens, llmwiretest.DeepOverhead)
	}
	if len(res.ResponseItems) == 0 {
		t.Fatal("the signed thinking turn did not come back for replay")
	}

	// Without the flag, or on a deployment with no deep model, the turn stays
	// on the chat model.
	if _, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if srv.Last().Model() != testModel {
		t.Fatalf("model = %q, want the chat model", srv.Last().Model())
	}
	plain := mustClient(t, Config{BaseURL: srv.URL, Timeout: 5 * time.Second}, nil)
	if _, err := plain.StreamChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if srv.Last().Model() != testModel || srv.Last().Reasoning() != llmwiretest.BalancedSent {
		t.Fatalf("no deep model: model %q reasoning %q, want the chat model at balanced", srv.Last().Model(), srv.Last().Reasoning())
	}
}

// A deep turn with an image stays on the deep model when it sees images, and
// falls back to the vision model when it does not.
func TestClient_DeepRoutingWithImages(t *testing.T) {
	withImage := toWireMessages([]Message{{
		Role:         "user",
		ContentParts: []MessageContentPart{{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,iVBORw0KGgo="}}},
	}})
	c := &Client{model: "chat", visionModel: "vision", deepModel: "deep", deepVision: true}
	if got := c.routeModel(withImage, true); got != "deep" {
		t.Fatalf("seeing deep model: %q, want deep", got)
	}
	c.deepVision = false
	if got := c.routeModel(withImage, true); got != "vision" {
		t.Fatalf("blind deep model: %q, want vision", got)
	}
	if got := c.routeModel(toWireMessages([]Message{{Role: "user", Content: "hi"}}), true); got != "deep" {
		t.Fatalf("text deep turn: %q, want deep", got)
	}
}

// A replayed assistant turn carries its signed thinking back to the wire.
func TestToWireMessages_CarriesResponseItems(t *testing.T) {
	item := json.RawMessage(`{"type":"thinking","thinking":"x","signature":"` + llmwiretest.ThinkingSignature + `"}`)
	out := toWireMessages([]Message{{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Type: "function",
		Function: ToolCallFunction{Name: "t", Arguments: "{}"}}}, ResponseItems: []json.RawMessage{item}}})
	if len(out) != 1 || len(out[0].ResponseItems) != 1 || string(out[0].ResponseItems[0]) != string(item) {
		t.Fatalf("wire message %+v lost the response items", out)
	}
}

// The deep model may be the chat model itself: its ordinary turns stay at the
// balanced level, only a turn that asked for deep thinks longer.
func TestClient_DeepModelSameAsChatAsksDeepOnlyWhenPicked(t *testing.T) {
	srv := llmwiretest.NewServer(t)
	client := mustClient(t, Config{BaseURL: srv.URL, Timeout: 5 * time.Second,
		Models: Roles{Chat: testModel, Deep: testModel}}, nil)
	if _, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := srv.Last().Reasoning(); got != llmwiretest.BalancedSent {
		t.Fatalf("standard turn: reasoning %q, want %q", got, llmwiretest.BalancedSent)
	}
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{Deep: true})
	if _, err := client.StreamChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := srv.Last().Reasoning(); got != llmwiretest.DeepSent {
		t.Fatalf("deep turn: reasoning %q, want %q", got, llmwiretest.DeepSent)
	}
}
