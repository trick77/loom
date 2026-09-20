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

// captureReasoningEffort runs one StreamChatWithTools turn against a stub endpoint
// and returns the reasoning_effort field of the outbound chat-completion request.
func captureReasoningEffort(ctx context.Context, t *testing.T) string {
	t.Helper()
	got := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded struct {
			ReasoningEffort string `json:"reasoning_effort"`
		}
		_ = json.Unmarshal(body, &decoded)
		got <- decoded.ReasoningEffort
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := mustClient(t, Config{BaseURL: server.URL, Timeout: 5 * time.Second}, server.Client())
	if _, err := client.StreamChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, nil, func(StreamEvent) error { return nil }); err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	return <-got
}

// The composer's per-request choice rides InferenceMetadata and must land in the
// outbound reasoning_effort field, overriding the client default.
func TestClient_StreamUsesReasoningEffortFromContext(t *testing.T) {
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{ReasoningEffort: "low"})
	if effort := captureReasoningEffort(ctx, t); effort != "low" {
		t.Fatalf("reasoning_effort = %q, want low", effort)
	}
}

// With no per-request effort (utility calls, or a client that never sends one),
// the turn falls back to the client's configured default.
func TestClient_StreamFallsBackToDefaultReasoningEffort(t *testing.T) {
	if effort := captureReasoningEffort(context.Background(), t); effort != DefaultReasoningEffort {
		t.Fatalf("reasoning_effort = %q, want %q", effort, DefaultReasoningEffort)
	}
}
