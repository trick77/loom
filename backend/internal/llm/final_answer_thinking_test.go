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

// streamRequestFields reads the budget from max_tokens: glm-5.3-flash accepts
// max_completion_tokens and then ignores it, so llmwire's profile renders the
// cap under max_tokens.
type streamRequestFields struct {
	Thinking        json.RawMessage `json:"thinking"`
	ReasoningEffort string          `json:"reasoning_effort"`
	MaxTokens       int             `json:"max_tokens"`
}

// captureStreamRequest runs one tool-free StreamChatWithTools turn against a stub
// endpoint and returns the thinking / reasoning_effort / max_tokens fields of the
// outbound chat-completion request, plus the resulting StreamResult.
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
// metadata. The outbound request must then ask for low effort (thinking cannot
// be disabled on this model) and carry the widened budget.
func TestClient_StreamSuppressesThinkingAndWidensBudgetFromContext(t *testing.T) {
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{
		SuppressThinking:    true,
		MaxCompletionTokens: 4096,
	})
	req, result := captureStreamRequest(ctx, t)
	if len(req.Thinking) > 0 {
		t.Fatalf("thinking = %s, want it absent (the model refuses the toggle)", req.Thinking)
	}
	if req.ReasoningEffort != "low" {
		t.Fatalf("reasoning_effort = %q, want low", req.ReasoningEffort)
	}
	if req.MaxTokens != 4096 {
		t.Fatalf("max_tokens = %d, want 4096", req.MaxTokens)
	}
	// The level that reached the wire is recorded for the metrics pill.
	if result.ReasoningEffort != "low" {
		t.Fatalf("result.ReasoningEffort = %q, want low", result.ReasoningEffort)
	}
}

// A normal turn (no overrides) asks for high effort and uses the client's default
// completion budget, unaffected by the override plumbing.
func TestClient_StreamKeepsThinkingWhenNotSuppressed(t *testing.T) {
	req, result := captureStreamRequest(context.Background(), t)
	if len(req.Thinking) > 0 {
		t.Fatalf("thinking = %s, want it absent", req.Thinking)
	}
	if req.ReasoningEffort != "high" {
		t.Fatalf("reasoning_effort = %q, want high", req.ReasoningEffort)
	}
	if result.ReasoningEffort != "high" {
		t.Fatalf("result.ReasoningEffort = %q, want high", result.ReasoningEffort)
	}
	if req.MaxTokens != defaultMaxCompletionTokens {
		t.Fatalf("max_tokens = %d, want default %d", req.MaxTokens, defaultMaxCompletionTokens)
	}
}

// The forced final answer runs on proseModel, and the routing must hold even
// when the gathered research carried an image. proseModel is the same
// deployment as textModel today; the seam stays so a model that earns its keep
// on synthesis moves this turn alone.
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
				t.Fatalf("model = %q, want %q", got, proseModel)
			}
		})
	}
}

// Off the suppressed path, routing is unchanged: vision model for an image
// part, text model otherwise.
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
