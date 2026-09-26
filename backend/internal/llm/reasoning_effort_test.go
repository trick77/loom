package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trick77/llmwire/llmwiretest"
)

// captureReasoning runs one StreamChatWithTools turn against a stub endpoint
// and reports the reasoning setting the result says was sent, and whether the
// outbound request carried a thinking object at all.
func captureReasoning(ctx context.Context, t *testing.T) (sent string, thinkingSent bool) {
	t.Helper()
	got := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded struct {
			Thinking json.RawMessage `json:"thinking"`
		}
		_ = json.Unmarshal(body, &decoded)
		got <- len(decoded.Thinking) > 0
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := mustClient(t, Config{BaseURL: server.URL, Timeout: 5 * time.Second}, server.Client())
	result, err := client.StreamChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, nil, func(StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	return result.ReasoningEffort, <-got
}

// A streamed turn asks for the model's balanced reasoning: a fast answer, not
// the deepest one the model can give. Which level that is belongs to the
// model's llmwire profile; the result records what was sent.
func TestClient_StreamAsksForBalancedReasoning(t *testing.T) {
	if sent, _ := captureReasoning(context.Background(), t); sent != llmwiretest.BalancedSent {
		t.Fatalf("reasoning sent = %q, want the balanced level %q", sent, llmwiretest.BalancedSent)
	}
}

// A retry of a turn that spent its whole budget reasoning asks for the least
// reasoning the model allows; the same request again would run out the same way.
func TestClient_LeastReasoningAsksForMinimal(t *testing.T) {
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{LeastReasoning: true})
	if sent, _ := captureReasoning(ctx, t); sent != llmwiretest.MinimalSent {
		t.Fatalf("reasoning sent = %q, want the minimal setting %q", sent, llmwiretest.MinimalSent)
	}
}

// The forced final answer reasons like any turn; only its budget widens.
func TestClient_ForcedFinalAnswerReasonsLikeATurn(t *testing.T) {
	ctx := WithInferenceMetadata(context.Background(), InferenceMetadata{MaxCompletionTokens: 4096})
	if sent, _ := captureReasoning(ctx, t); sent != llmwiretest.BalancedSent {
		t.Fatalf("reasoning sent = %q, want %q", sent, llmwiretest.BalancedSent)
	}
}
