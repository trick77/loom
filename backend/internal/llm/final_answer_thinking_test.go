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

type streamRequestFields struct {
	Thinking            *thinkingOption `json:"thinking"`
	ReasoningEffort     string          `json:"reasoning_effort"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
}

// captureStreamRequest runs one tool-free StreamChatWithTools turn against a stub
// endpoint and returns the thinking / reasoning_effort / max_completion_tokens
// fields of the outbound chat-completion request, plus the resulting StreamResult.
func captureStreamRequest(t *testing.T, ctx context.Context) (streamRequestFields, StreamResult) {
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

	client := NewClient(Config{BaseURL: server.URL, Timeout: 5 * time.Second}, server.Client())
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
		ReasoningEffort:     "high",
		SuppressThinking:    true,
		MaxCompletionTokens: 4096,
	})
	req, result := captureStreamRequest(t, ctx)
	if req.Thinking == nil || req.Thinking.Type != "disabled" {
		t.Fatalf("thinking = %+v, want {type:disabled}", req.Thinking)
	}
	if req.ReasoningEffort != "" {
		t.Fatalf("reasoning_effort = %q, want empty on the wire when thinking is suppressed", req.ReasoningEffort)
	}
	if req.MaxCompletionTokens != 4096 {
		t.Fatalf("max_completion_tokens = %d, want 4096", req.MaxCompletionTokens)
	}
	// The wire drops reasoning_effort, but the StreamResult must keep the composer's
	// chosen effort so the persisted message doesn't lose its reasoning-effort badge.
	if result.ReasoningEffort != "high" {
		t.Fatalf("result.ReasoningEffort = %q, want high (preserved for the persisted message)", result.ReasoningEffort)
	}
}

// A normal turn (no overrides) keeps thinking on — no thinking field is sent — and
// uses the client's default completion budget, unaffected by the override plumbing.
func TestClient_StreamKeepsThinkingWhenNotSuppressed(t *testing.T) {
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{ReasoningEffort: "high"})
	req, _ := captureStreamRequest(t, ctx)
	if req.Thinking != nil {
		t.Fatalf("thinking = %+v, want nil for a normal turn", req.Thinking)
	}
	if req.ReasoningEffort != "high" {
		t.Fatalf("reasoning_effort = %q, want high", req.ReasoningEffort)
	}
	if req.MaxCompletionTokens != defaultMaxCompletionTokens {
		t.Fatalf("max_completion_tokens = %d, want default %d", req.MaxCompletionTokens, defaultMaxCompletionTokens)
	}
}
