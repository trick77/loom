package llm

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// recordCapture records every slog record so a test can assert a specific log
// line (by message) was emitted with the expected attributes.
type recordCapture struct {
	mu      sync.Mutex
	records []captured
}

type captured struct {
	msg   string
	attrs map[string]slog.Value
	level slog.Level
}

func (h *recordCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := captured{msg: r.Message, level: r.Level, attrs: map[string]slog.Value{}}
	r.Attrs(func(a slog.Attr) bool { c.attrs[a.Key] = a.Value; return true })
	h.records = append(h.records, c)
	return nil
}
func (h *recordCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordCapture) WithGroup(string) slog.Handler      { return h }

// levels lists the level of every record with the message, in order.
func (h *recordCapture) levels(msg string) []slog.Level {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []slog.Level
	for _, c := range h.records {
		if c.msg == msg {
			out = append(out, c.level)
		}
	}
	return out
}

// The early tool_call event exists so the UI can show the running tool at
// once; it must carry the call's id, because the client keys the trace row on
// it and the full call re-emitted at end-of-stream would otherwise land as a
// second row. When the name arrives a chunk before the id, wait for the id.
func TestClient_EarlyToolCallEventWaitsForID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(s string) {
			_, _ = w.Write([]byte(s))
			if flusher != nil {
				flusher.Flush()
			}
		}
		write(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"fetch__fetch"}}]}}]}` + "\n\n")
		write(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1"}]}}]}` + "\n\n")
		write(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"url\":\"https://example.com\"}"}}]}}]}` + "\n\n")
		write(`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n")
		write("data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	client := mustClient(t, Config{BaseURL: server.URL, Timeout: 5 * time.Second, IdleTimeout: 2 * time.Second}, server.Client())

	var early []ToolCall
	_, err := client.StreamChatWithTools(
		context.Background(),
		[]Message{{Role: "user", Content: "fetch example.com"}},
		[]Tool{{Type: "function", Function: ToolFunction{Name: "fetch__fetch"}}},
		func(event StreamEvent) error {
			if event.ToolCall.Function.Name != "" && event.ToolCall.Function.Arguments == "" {
				early = append(early, event.ToolCall)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("StreamChatWithTools() error = %v", err)
	}
	if len(early) != 1 {
		t.Fatalf("early tool_call events = %d, want exactly 1: %+v", len(early), early)
	}
	if early[0].ID != "call_1" {
		t.Fatalf("early tool_call id = %q, want call_1", early[0].ID)
	}
}
