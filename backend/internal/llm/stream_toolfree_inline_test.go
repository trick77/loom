package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The forced final-answer call is made with no tools (to stop tool-calling). MiMo
// sometimes ignores that and emits inline <tool_call> XML as its "answer". With no
// tools on offer the inline gate used to be disabled, so the raw markup streamed
// straight to the client and was persisted verbatim. A MiMo turn must gate and
// strip that markup regardless of whether tools were offered, so raw XML never
// leaks as the answer.
func TestClient_InlineToolCallLeaksOnToolFreeMiMoCall(t *testing.T) {
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
		write("data: {\"choices\":[{\"delta\":{\"content\":" + jsonString(block) + "}}]}\n\n")
		write("data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Timeout: 5 * time.Second, IdleTimeout: 2 * time.Second}, server.Client())

	var contentSeen strings.Builder
	// nil tools: the forced final-answer call.
	result, err := client.StreamChatWithTools(
		context.Background(),
		[]Message{{Role: "user", Content: "answer now"}},
		nil,
		func(event StreamEvent) error {
			contentSeen.WriteString(event.Delta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("StreamChatWithTools() error = %v", err)
	}

	if strings.Contains(contentSeen.String(), "<tool_call>") {
		t.Fatalf("raw <tool_call> markup leaked to client content: %q", contentSeen.String())
	}
	if strings.Contains(result.Content, "<tool_call>") {
		t.Fatalf("raw <tool_call> markup persisted in result content: %q", result.Content)
	}
}
