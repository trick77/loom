package llm

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// MiMo sometimes emits a tool call as inline XML in the reasoning_content channel
// rather than in content or as native tool_calls. That channel is streamed raw to
// the client and was never parsed for tool calls, so the markup leaked into the
// visible reasoning and no tool ran — a silent dead end. The reasoning channel must
// be gated (no raw <tool_call> markup reaches the client) and parsed at end-of-stream
// just like the content channel.
func TestClient_InlineToolCallInReasoningChannelIsParsedAndGated(t *testing.T) {
	// A leading bit of genuine reasoning, then the inline tool-call block — both in
	// reasoning_content, exactly as the failing transcript showed (single-line block).
	const block = "<tool_call> <function=fetch__fetch> <parameter=url>https://example.com</parameter> </function> </tool_call>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(s string) {
			_, _ = w.Write([]byte(s))
			if flusher != nil {
				flusher.Flush()
			}
		}
		write("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"Let me fetch that. \"}}]}\n\n")
		write("data: {\"choices\":[{\"delta\":{\"reasoning_content\":" + jsonString(block) + "}}]}\n\n")
		write("data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo", Timeout: 5 * time.Second, IdleTimeout: 2 * time.Second}, server.Client())

	var reasoningSeen strings.Builder
	result, err := client.StreamChatWithTools(
		context.Background(),
		[]Message{{Role: "user", Content: "fetch example.com"}},
		[]Tool{{Type: "function", Function: ToolFunction{Name: "fetch__fetch"}}},
		func(event StreamEvent) error {
			reasoningSeen.WriteString(event.ReasoningDelta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("StreamChatWithTools() error = %v", err)
	}

	// The tool call hidden in the reasoning channel must be recovered.
	if len(result.ToolCalls) != 1 {
		t.Fatalf("result.ToolCalls = %d, want 1 (inline call in reasoning channel must be parsed)", len(result.ToolCalls))
	}
	if got := result.ToolCalls[0].Function.Name; got != "fetch__fetch" {
		t.Fatalf("tool call name = %q, want fetch__fetch", got)
	}
	if got := result.ToolCalls[0].Function.Arguments; !strings.Contains(got, "https://example.com") {
		t.Fatalf("tool call arguments = %q, want the url", got)
	}

	// The raw <tool_call> markup must never reach the client as reasoning.
	if strings.Contains(reasoningSeen.String(), "<tool_call>") {
		t.Fatalf("raw <tool_call> markup leaked to client reasoning: %q", reasoningSeen.String())
	}
	// Legitimate reasoning before the marker must still surface.
	if !strings.Contains(reasoningSeen.String(), "Let me fetch that.") {
		t.Fatalf("legitimate reasoning before the marker was dropped: %q", reasoningSeen.String())
	}
}

// An inline tool call recovered from a non-native channel is invisible in the
// completion log's tool=/tool_arg_bytes fields (those read the native tool_calls
// map only). It must therefore be logged distinctly, naming the channel it came
// from, so the phenomenon is diagnosable from logs rather than silent.
func TestClient_RecoveredInlineToolCallIsLoggedWithChannel(t *testing.T) {
	const block = "<tool_call> <function=fetch__fetch> <parameter=url>https://example.com</parameter> </function> </tool_call>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		write := func(s string) {
			_, _ = w.Write([]byte(s))
			if flusher != nil {
				flusher.Flush()
			}
		}
		write("data: {\"choices\":[{\"delta\":{\"reasoning_content\":" + jsonString(block) + "}}]}\n\n")
		write("data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	capture := &recordCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(prev) })

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo", Timeout: 5 * time.Second, IdleTimeout: 2 * time.Second}, server.Client())
	if _, err := client.StreamChatWithTools(
		context.Background(),
		[]Message{{Role: "user", Content: "fetch example.com"}},
		[]Tool{{Type: "function", Function: ToolFunction{Name: "fetch__fetch"}}},
		nil,
	); err != nil {
		t.Fatalf("StreamChatWithTools() error = %v", err)
	}

	rec, ok := capture.find("recovered inline tool calls")
	if !ok {
		t.Fatalf("no %q log line emitted; captured: %v", "recovered inline tool calls", capture.messages())
	}
	if got := rec["channel"].String(); got != "reasoning" {
		t.Fatalf("log channel = %q, want reasoning", got)
	}
}

// jsonString renders s as a JSON string literal (with surrounding quotes) for
// embedding in a hand-built SSE chunk.
func jsonString(s string) string {
	var b strings.Builder
	writeJSONString(&b, s)
	return b.String()
}

// recordCapture records every slog record so a test can assert a specific log
// line (by message) was emitted with the expected attributes.
type recordCapture struct {
	mu      sync.Mutex
	records []captured
}

type captured struct {
	msg   string
	attrs map[string]slog.Value
}

func (h *recordCapture) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordCapture) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := captured{msg: r.Message, attrs: map[string]slog.Value{}}
	r.Attrs(func(a slog.Attr) bool { c.attrs[a.Key] = a.Value; return true })
	h.records = append(h.records, c)
	return nil
}
func (h *recordCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordCapture) WithGroup(string) slog.Handler      { return h }

func (h *recordCapture) find(msg string) (map[string]slog.Value, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.records {
		if c.msg == msg {
			return c.attrs, true
		}
	}
	return nil, false
}

func (h *recordCapture) messages() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, c := range h.records {
		out = append(out, c.msg)
	}
	return out
}
