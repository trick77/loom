package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// MiMo streams the inline tool name right after the <tool_call> marker but only
// flushes the (large) argument tens of seconds later. The stream must surface the
// name early — as a ToolCall event with an empty argument — so the client can show
// which tool is running during the silent gap, then update that same entry (same id)
// when the full call lands at end-of-stream.
func TestClient_StreamSurfacesInlineToolNameBeforeArguments(t *testing.T) {
	chunks := []string{
		`data: {"choices":[{"delta":{"content":"Sure.<tool_call>"}}]}`,
		`data: {"choices":[{"delta":{"content":"\n<function=create_pdf_file>\n"}}]}`,
		`data: {"choices":[{"delta":{"content":"<parameter=content>Hello body</parameter>\n</function>\n</tool_call>"}}]}`,
		`data: [DONE]`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			_, _ = w.Write([]byte(c + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(5 * time.Millisecond)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo", Timeout: 5 * time.Second, IdleTimeout: 2 * time.Second}, server.Client())
	tools := []Tool{{Type: "function", Function: ToolFunction{Name: "create_pdf_file"}}}

	var toolCallEvents []ToolCall
	pendingBeforeFirstToolCall := false
	sawPending := false
	_, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "make a pdf"}}, tools, func(event StreamEvent) error {
		if event.ToolPending {
			sawPending = true
		}
		if event.ToolCall.Function.Name != "" {
			if len(toolCallEvents) == 0 && sawPending {
				pendingBeforeFirstToolCall = true
			}
			toolCallEvents = append(toolCallEvents, event.ToolCall)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}

	if len(toolCallEvents) < 2 {
		t.Fatalf("want at least 2 tool-call events (early name + final full call), got %d: %#v", len(toolCallEvents), toolCallEvents)
	}
	if !pendingBeforeFirstToolCall {
		t.Fatalf("tool_pending must precede the first tool-call event")
	}

	early := toolCallEvents[0]
	if early.Function.Name != "create_pdf_file" {
		t.Fatalf("early tool-call name = %q, want create_pdf_file", early.Function.Name)
	}
	if early.Function.Arguments != "" {
		t.Fatalf("early tool-call must carry no argument yet, got %q", early.Function.Arguments)
	}
	if early.ID != inlineToolCallID(0) {
		t.Fatalf("early tool-call id = %q, want %q so the final call updates the same entry", early.ID, inlineToolCallID(0))
	}

	final := toolCallEvents[len(toolCallEvents)-1]
	if final.ID != early.ID {
		t.Fatalf("final tool-call id = %q, want same as early %q (no duplicate trace entry)", final.ID, early.ID)
	}
	if final.Function.Arguments == "" {
		t.Fatalf("final tool-call must carry the parsed argument, got empty")
	}
}
