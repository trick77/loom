package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/lume/internal/chat"
	"github.com/trick77/lume/internal/llm"
)

// A tool call truncated at the completion-token cap (finish_reason=length) must
// not be appended to history and replayed into the next round: the model emits a
// document as a single tool-call argument, so a truncated call is invalid JSON
// that the document tool cannot parse and that makes the upstream reject the next
// round's prefill (HTTP 500 → generic "stream failed"). The loop must stop at the
// truncated round with a clear, user-facing cause.
func TestStreamMessageStopsOnTruncatedToolCall(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing"},
	}
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "create_text_file",
						Arguments: `{"filename":"spec.md","content":"# Spec\n\ntruncat`,
					},
				}},
				FinishReason: "length",
			},
			// No second result: were the loop to wrongly continue into round 2, the
			// fake would panic on the empty results slice — an extra guard that the
			// truncated call is never replayed.
		},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
		MCP: fakeMCPService{
			tools: []llm.Tool{{
				Type:     "function",
				Function: llm.ToolFunction{Name: "search__web", Description: "Search", Parameters: map[string]any{"type": "object"}},
			}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Write a huge spec to a file"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "cut off") {
		t.Fatalf("SSE body missing clear truncation error:\n%s", body)
	}
	if strings.Contains(body, "stream failed") {
		t.Fatalf("truncated tool call surfaced as generic 'stream failed':\n%s", body)
	}
	// The loop must stop at round 1: exactly one model call, no replayed round.
	if len(llmClient.histories) != 1 {
		t.Fatalf("model called %d times, want exactly 1 (no round 2 on truncation)", len(llmClient.histories))
	}
	// Only the user message persists; the broken assistant turn is discarded.
	if len(store.messages) != 1 || store.messages[0].Role != chat.RoleUser {
		t.Fatalf("persisted messages = %#v, want only the user message", store.messages)
	}
}
