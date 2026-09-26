package llm

import (
	"context"
	"testing"
	"time"

	"github.com/trick77/llmwire/llmwiretest"
)

// The forced-final answer turn sets MaxCompletionTokens on the metadata; the
// outbound request carries that widened budget instead of the client default.
func TestClient_StreamWidensBudgetFromContext(t *testing.T) {
	srv := llmwiretest.NewServer(t)
	client := mustClient(t, Config{BaseURL: srv.URL, Timeout: 5 * time.Second}, nil)
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{MaxCompletionTokens: 4096})

	if _, err := client.StreamChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, nil, func(StreamEvent) error { return nil }); err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if got, ok := srv.Last().MaxTokens(); !ok || got != 4096 {
		t.Fatalf("cap = %d (sent %v), want 4096", got, ok)
	}
}

// A normal turn uses the client's default completion budget.
func TestClient_StreamUsesTheDefaultBudget(t *testing.T) {
	srv := llmwiretest.NewServer(t)
	client := mustClient(t, Config{BaseURL: srv.URL, Timeout: 5 * time.Second}, nil)

	if _, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, func(StreamEvent) error { return nil }); err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if got, ok := srv.Last().MaxTokens(); !ok || got != defaultMaxCompletionTokens {
		t.Fatalf("cap = %d (sent %v), want default %d", got, ok, defaultMaxCompletionTokens)
	}
}

// Routing: the vision model iff the payload carries an image part, the chat
// model otherwise.
func TestClient_StreamRoutesImagesToTheVisionModel(t *testing.T) {
	c := &Client{model: "chat-model", visionModel: "vision-model", gateModel: "gate-model"}
	if got := c.modelForMessages([]Message{{Role: "user", Content: "hi"}}); got != "chat-model" {
		t.Fatalf("model = %q, want the chat model", got)
	}
	withImage := []Message{{
		Role:         "user",
		ContentParts: []MessageContentPart{{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,iVBORw0KGgo="}}},
	}}
	if got := c.modelForMessages(withImage); got != "vision-model" {
		t.Fatalf("model = %q, want the vision model", got)
	}
}
