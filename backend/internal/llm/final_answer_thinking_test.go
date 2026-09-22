package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// thinkingOption mirrors MiMo's native switch as llmwire renders it.
type thinkingOption struct {
	Type string `json:"type"`
}

type streamRequestFields struct {
	Thinking            *thinkingOption `json:"thinking"`
	ReasoningEffort     string          `json:"reasoning_effort"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
}

// captureStreamRequest runs one tool-free StreamChatWithTools turn against a stub
// endpoint and returns the thinking / reasoning_effort / max_completion_tokens
// fields of the outbound chat-completion request, plus the resulting StreamResult.
func captureStreamRequest(ctx context.Context, t *testing.T) (streamRequestFields, StreamResult) {
	t.Helper()
	got := make(chan streamRequestFields, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded streamRequestFields
		_ = json.Unmarshal(body, &decoded)
		got <- decoded
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := mustClient(t, Config{BaseURL: server.URL, Timeout: 5 * time.Second}, server.Client())
	result, err := client.StreamChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, nil, func(StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	return <-got, result
}

// The forced-final answer turn sets SuppressThinking + MaxCompletionTokens on the
// metadata. The outbound request must then disable thinking, drop reasoning_effort
// (the two directives would otherwise conflict), and carry the widened budget.
func TestClient_StreamSuppressesThinkingAndWidensBudgetFromContext(t *testing.T) {
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{
		SuppressThinking:    true,
		MaxCompletionTokens: 4096,
	})
	req, result := captureStreamRequest(ctx, t)
	if req.Thinking == nil || req.Thinking.Type != "disabled" {
		t.Fatalf("thinking = %+v, want {type:disabled}", req.Thinking)
	}
	if req.ReasoningEffort != "" {
		t.Fatalf("reasoning_effort = %q, want empty on the wire when thinking is suppressed", req.ReasoningEffort)
	}
	if req.MaxCompletionTokens != 4096 {
		t.Fatalf("max_completion_tokens = %d, want 4096", req.MaxCompletionTokens)
	}
	// Nothing to preserve: no effort is sent on any turn, so none is recorded.
	if result.ReasoningEffort != "" {
		t.Fatalf("result.ReasoningEffort = %q, want it blank", result.ReasoningEffort)
	}
}

// A normal turn (no overrides) keeps thinking on — no thinking field is sent — and
// uses the client's default completion budget, unaffected by the override plumbing.
func TestClient_StreamKeepsThinkingWhenNotSuppressed(t *testing.T) {
	req, _ := captureStreamRequest(context.Background(), t)
	if req.Thinking != nil {
		t.Fatalf("thinking = %+v, want nil for a normal turn", req.Thinking)
	}
	if req.ReasoningEffort != "" {
		t.Fatalf("reasoning_effort = %q, want it absent", req.ReasoningEffort)
	}
	if req.MaxCompletionTokens != defaultMaxCompletionTokens {
		t.Fatalf("max_completion_tokens = %d, want default %d", req.MaxCompletionTokens, defaultMaxCompletionTokens)
	}
}

// The forced final answer runs on proseModel, not flash, and the routing must
// hold even when the gathered research carried an image.
//
// This is the correctness rule, not a preference: the forced final answer
// writes the answer the user actually asked for, with thinking disabled, and
// mimo-v2.6-flash answered a one-step arithmetic prompt wrong in three runs of
// three in that mode while Pro was correct in all three. "Total these three
// figures" over gathered research is an ordinary request. A regression that
// routes this turn to flash is silent: the answer still streams, it is just
// wrong.
func TestClient_StreamRoutesTheSuppressedTurnToTheProseModel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		messages []Message
	}{
		{"text only", []Message{{Role: "user", Content: "hi"}}},
		{"carrying an image", []Message{{
			Role:         "user",
			ContentParts: []MessageContentPart{{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,iVBORw0KGgo="}}},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{model: textModel, visionModel: visionModel, shortGateModel: shortGateModel, proseModel: proseModel}
			if got := c.modelForMessages(tc.messages, true); got != proseModel {
				t.Fatalf("model = %q, want %q (the suppressed turn must not run on flash)", got, proseModel)
			}
		})
	}
}

// With thinking on, routing is unchanged: vision model for an image part, text
// model otherwise. The prose model is reserved for the thinking-off case.
func TestClient_StreamRoutingIsUnchangedWhenThinkingIsOn(t *testing.T) {
	c := &Client{model: textModel, visionModel: visionModel, shortGateModel: shortGateModel, proseModel: proseModel}
	if got := c.modelForMessages([]Message{{Role: "user", Content: "hi"}}, false); got != textModel {
		t.Fatalf("model = %q, want %q", got, textModel)
	}
	withImage := []Message{{
		Role:         "user",
		ContentParts: []MessageContentPart{{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,iVBORw0KGgo="}}},
	}}
	if got := c.modelForMessages(withImage, false); got != visionModel {
		t.Fatalf("model = %q, want %q", got, visionModel)
	}
}
