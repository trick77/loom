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

// A normal turn asks for "high". glm-5.3-flash always thinks and takes only
// low/high/max; sending nothing gets the vendor default max, which peeq
// measured at ~5x the wall-clock of high (69.9s vs 12.8s) for the same job.
// A regression that drops the field silently puts every turn back on max.
func TestClient_StreamSendsHighReasoningEffort(t *testing.T) {
	effort, _ := captureReasoningFields(context.Background(), t)
	if effort != "high" {
		t.Fatalf("reasoning_effort = %q, want high", effort)
	}
}

// No thinking object on any streamed turn: the model refuses the disable
// toggle outright (400, code 1210), so the effort level is the only lever.
func TestClient_StreamNeverSendsAThinkingObject(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{"normal turn", context.Background()},
		{"suppressed turn", WithInferenceMetadata(context.Background(), InferenceMetadata{SuppressThinking: true})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, thinkingSent := captureReasoningFields(tc.ctx, t); thinkingSent {
				t.Fatal("a thinking object was sent; glm-5.3-flash refuses it")
			}
		})
	}
}

// The forced final answer asks for the shallowest level the model accepts:
// thinking cannot be switched off, and deep thinking there burns the whole
// completion budget and emits no prose.
func TestClient_StreamSendsLowEffortWhenSuppressed(t *testing.T) {
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{SuppressThinking: true})
	if effort, _ := captureReasoningFields(ctx, t); effort != "low" {
		t.Fatalf("reasoning_effort = %q, want low", effort)
	}
}
