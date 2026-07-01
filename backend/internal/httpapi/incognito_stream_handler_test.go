package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
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

// incognitoRetryStub models a tool-eager model (MiMo): its first turn returns empty
// content with a recovered inline tool call, its second (the retry) returns real
// prose. This exercises runIncognitoAssistantTurn's empty-answer safety net.
type incognitoRetryStub struct {
	fakeChatClient
	calls int
	// firstTurnNoToolCall models the truncated/malformed inline-markup case: empty
	// content with NO recovered tool call. The retry must still fire.
	firstTurnNoToolCall bool
}

func (f *incognitoRetryStub) StreamChatWithTools(ctx context.Context, history []llm.Message, _ []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	f.calls++
	if f.calls == 1 {
		// The inline tool-call markup was already stripped upstream, so content is
		// empty — the exact shape that used to yield an "empty incognito assistant
		// response". A well-formed call leaves a recovered ToolCall; a
		// truncated/malformed one leaves none. Both must trigger the retry.
		if f.firstTurnNoToolCall {
			return llm.StreamResult{Content: ""}, nil
		}
		return llm.StreamResult{
			Content:   "",
			ToolCalls: []llm.ToolCall{{ID: "inline_call_1", Type: "function", Function: llm.ToolCallFunction{Name: "search"}}},
		}, nil
	}
	answer := "Penguins are flightless seabirds."
	if onEvent != nil {
		if err := onEvent(llm.StreamEvent{Delta: answer}); err != nil {
			return llm.StreamResult{}, err
		}
	}
	return llm.StreamResult{Content: answer}, nil
}

// TestIncognitoStreamRetriesWhenFirstTurnEmpty verifies the safety net: when the
// first tool-free turn comes back empty (model emitted a stripped inline tool call),
// the handler nudges once and returns the retried answer instead of erroring.
func TestIncognitoStreamRetriesWhenFirstTurnEmpty(t *testing.T) {
	stub := &incognitoRetryStub{}
	srv := newAuthenticatedServer(t, Deps{Thread: &fakeThreadStore{}, LLM: stub})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/incognito/messages:stream", `{"content":"Tell me about penguins"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Penguins are flightless seabirds.") {
		t.Fatalf("expected retried answer in stream, got:\n%s", body)
	}
	if !strings.Contains(body, "event: assistant_message") || strings.Contains(body, "empty assistant response") {
		t.Fatalf("expected a delivered assistant message, not an empty-response error:\n%s", body)
	}
	if stub.calls != 2 {
		t.Fatalf("model calls = %d, want 2 (initial + one retry)", stub.calls)
	}
}

// TestIncognitoStreamRetriesWhenFirstTurnEmptyNoToolCall covers the malformed/
// truncated inline-markup case: empty content with NO recovered tool call must
// still trigger the retry (the gate is empty-content alone).
func TestIncognitoStreamRetriesWhenFirstTurnEmptyNoToolCall(t *testing.T) {
	stub := &incognitoRetryStub{firstTurnNoToolCall: true}
	srv := newAuthenticatedServer(t, Deps{Thread: &fakeThreadStore{}, LLM: stub})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/incognito/messages:stream", `{"content":"Tell me about penguins"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Penguins are flightless seabirds.") {
		t.Fatalf("expected retried answer in stream, got:\n%s", body)
	}
	if strings.Contains(body, "empty assistant response") {
		t.Fatalf("empty content with no recovered call must retry, not error:\n%s", body)
	}
	if stub.calls != 2 {
		t.Fatalf("model calls = %d, want 2 (initial + one retry)", stub.calls)
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
