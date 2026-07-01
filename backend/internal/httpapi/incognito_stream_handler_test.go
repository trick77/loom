package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

// TestIncognitoStreamEmitsReplyAndPersistsNothing verifies the incognito endpoint
// streams a normal answer while writing nothing to the store: no messages, no
// created thread, no directives.
func TestIncognitoStreamEmitsReplyAndPersistsNothing(t *testing.T) {
	store := &fakeThreadStore{}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/incognito/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"event: assistant_delta",
		`data: {"content":"Hel"}`,
		"event: assistant_message",
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	// The persisted transcript must be untouched — nothing incognito is stored.
	if len(store.messages) != 0 {
		t.Fatalf("persisted messages = %d, want 0 (incognito must not persist)", len(store.messages))
	}
	if store.assistantContent != "" {
		t.Fatalf("assistantContent = %q, want empty (no assistant message persisted)", store.assistantContent)
	}
	if len(store.userDirectives) != 0 {
		t.Fatalf("userDirectives = %d, want 0 (incognito must not write memory)", len(store.userDirectives))
	}
	// A title/thread event is emitted only on the persisted path; incognito never
	// creates or titles a thread.
	if strings.Contains(body, "event: thread") {
		t.Fatalf("incognito must not emit a thread event:\n%s", body)
	}
}

// TestIncognitoStreamRejectsEmptyContent guards the content validation.
func TestIncognitoStreamRejectsEmptyContent(t *testing.T) {
	srv := newAuthenticatedServer(t, Deps{
		Thread: &fakeThreadStore{},
		LLM:    fakeChatClient{},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/incognito/messages:stream", `{"content":"  "}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// TestIncognitoStreamUsesClientHistory verifies prior turns supplied by the client
// are folded into the model history (the server keeps none of its own).
func TestIncognitoStreamUsesClientHistory(t *testing.T) {
	history := incognitoPriorMessages([]incognitoHistoryEntry{
		{Role: "user", Content: "earlier question"},
		{Role: "assistant", Content: "earlier answer"},
		{Role: "tool", Content: "dropped"},
	})
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2 (tool role dropped)", len(history))
	}
	if history[0].Role != chat.RoleUser || history[1].Role != chat.RoleAssistant {
		t.Fatalf("history roles = %v/%v, want user/assistant", history[0].Role, history[1].Role)
	}
}
