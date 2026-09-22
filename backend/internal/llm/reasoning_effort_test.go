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

// captureReasoningFields runs one StreamChatWithTools turn against a stub
// endpoint and reports what the outbound chat-completion request carried for
// reasoning: the reasoning_effort field, and whether a thinking object was sent
// at all.
func captureReasoningFields(ctx context.Context, t *testing.T) (effort string, thinkingSent bool) {
	t.Helper()
	type captured struct {
		effort   string
		thinking bool
	}
	got := make(chan captured, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded struct {
			ReasoningEffort string          `json:"reasoning_effort"`
			Thinking        json.RawMessage `json:"thinking"`
		}
		_ = json.Unmarshal(body, &decoded)
		got <- captured{effort: decoded.ReasoningEffort, thinking: len(decoded.Thinking) > 0}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := mustClient(t, Config{BaseURL: server.URL, Timeout: 5 * time.Second}, server.Client())
	if _, err := client.StreamChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, nil, func(StreamEvent) error { return nil }); err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	c := <-got
	return c.effort, c.thinking
}

// No reasoning_effort on the wire, ever. The levels are inert on this model
// family — measured five samples per level, every range overlapping every
// other, with "high" the lowest mean of the three on Flash — and the no-level
// distribution reaches deeper than any level's, so sending one only flattens
// the model's own judgement. Omitting is deliberate, not an oversight: a
// regression that reintroduces the field would silently cap how deep a turn
// can think.
func TestClient_StreamSendsNoReasoningEffort(t *testing.T) {
	effort, _ := captureReasoningFields(context.Background(), t)
	if effort != "" {
		t.Fatalf("reasoning_effort = %q, want it absent", effort)
	}
}

// A normal turn sends no thinking object either: thinking is on by default at
// the endpoint, and loom only ever reaches for the toggle to turn it OFF.
func TestClient_StreamLeavesThinkingUnsetOnANormalTurn(t *testing.T) {
	if _, thinkingSent := captureReasoningFields(context.Background(), t); thinkingSent {
		t.Fatal("a thinking object was sent on a normal turn; only SuppressThinking should set one")
	}
}

// The forced final answer is the one streaming path that disables thinking:
// leaving it on lets the model burn the whole completion budget reasoning and
// emit no prose. It still sends no effort level.
func TestClient_StreamDisablesThinkingWhenSuppressed(t *testing.T) {
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{SuppressThinking: true})
	effort, thinkingSent := captureReasoningFields(ctx, t)
	if !thinkingSent {
		t.Fatal("SuppressThinking did not send a thinking object")
	}
	if effort != "" {
		t.Fatalf("reasoning_effort = %q, want it absent even when thinking is off", effort)
	}
}
