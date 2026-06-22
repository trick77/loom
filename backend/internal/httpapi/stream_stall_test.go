package httpapi

import (
	"context"
	"errors"
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
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM: fakeChatClient{
			reasoningText: "thinking",
			streamErr:     fmt.Errorf("read chat completion stream: %w", llm.ErrStreamStalled),
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
	if len(store.messages) != 1 || store.messages[0].Role != chat.RoleUser {
		t.Fatalf("persisted messages = %#v, want only the user message", store.messages)
	}
}

func TestPersistInterruptedPartial(t *testing.T) {
	stalled := fmt.Errorf("read chat completion stream: %w", llm.ErrStreamStalled)
	cases := []struct {
		name   string
		result llm.StreamResult
		err    error
		want   bool
	}{
		{"client cancel with content", llm.StreamResult{Content: "partial"}, context.Canceled, true},
		{"stall with content", llm.StreamResult{Content: "partial"}, stalled, true},
		{"stall reasoning only", llm.StreamResult{ReasoningContent: "thought"}, stalled, false},
		{"cancel no content", llm.StreamResult{}, context.Canceled, false},
		{"unrelated error with content", llm.StreamResult{Content: "partial"}, errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := persistInterruptedPartial(tc.result, tc.err); got != tc.want {
				t.Fatalf("persistInterruptedPartial = %v, want %v", got, tc.want)
			}
		})
	}
}
