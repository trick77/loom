package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// A stalled upstream (idle watchdog abort) must surface a clear cause to the
// client instead of the generic "stream failed", and must not silently drop the
// turn the way a client disconnect (context.Canceled) does.
func TestStreamMessageSurfacesStallAsClearError(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			ReasoningText: "thinking",
			StreamErr:     fmt.Errorf("read chat completion stream: %w", llm.ErrStreamStalled),
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"error":"`+llm.ErrStreamStalled.Error()+`"`) {
		t.Fatalf("SSE body missing clear stall error:\n%s", body)
	}
	if strings.Contains(body, "stream failed") {
		t.Fatalf("stall surfaced as generic 'stream failed':\n%s", body)
	}
	if len(store.Messages) != 1 || store.Messages[0].Role != chat.RoleUser {
		t.Fatalf("persisted messages = %#v, want only the user message", store.Messages)
	}
}
