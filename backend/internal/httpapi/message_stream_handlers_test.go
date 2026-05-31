package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/llm"
)

func TestStreamMessageEmitsDeltasAndPersistsAssistant(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{title: "Greeting"},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"event: user_message",
		"event: assistant_delta",
		`data: {"content":"Hel"}`,
		"event: assistant_message",
		"event: thread",
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	if store.assistantContent != "Hello" {
		t.Fatalf("assistantContent = %q, want Hello", store.assistantContent)
	}
	if len(store.messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.messages))
	}
	if store.messages[0].Role != chat.RoleUser || store.messages[0].Content != "Hi" {
		t.Fatalf("first persisted message = %#v, want user Hi", store.messages[0])
	}
	if store.messages[1].Role != chat.RoleAssistant || store.messages[1].Content != "Hello" {
		t.Fatalf("second persisted message = %#v, want assistant Hello", store.messages[1])
	}
}

func TestStreamMessagePersistsAssistantAfterClientContextCancel(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	var cancel context.CancelFunc
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM: fakeChatClient{afterStream: func() {
			cancel()
		}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)
	ctx, cancelRequest := context.WithCancel(req.Context())
	cancel = cancelRequest
	req = req.WithContext(ctx)

	srv.ServeHTTP(rec, req)

	if store.assistantContent != "Hello" {
		t.Fatalf("assistantContent = %q, want Hello", store.assistantContent)
	}
	if store.assistantContextErr != nil {
		t.Fatalf("assistant AddMessage context error = %v, want nil", store.assistantContextErr)
	}
}

func TestStreamMessageBuildsResponseLanguageHistory(t *testing.T) {
	var history []llm.Message
	store := &fakeChatStore{
		thread:   chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
		messages: []chat.Message{{ID: "old_1", ThreadID: "thr_1", Role: chat.RoleAssistant, Content: "Earlier answer"}},
	}
	user := testUser
	user.ResponseLanguage = "de"
	srv := newAuthenticatedChatServerForUser(t, user, Deps{
		Chat: store,
		LLM:  fakeChatClient{history: &history},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Neue Frage"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(history) != 3 {
		t.Fatalf("history len = %d, want 3: %#v", len(history), history)
	}
	if !strings.Contains(history[0].Content, "Always answer in this language: de.") {
		t.Fatalf("system prompt = %q, want response-language directive", history[0].Content)
	}
	if history[1] != (llm.Message{Role: "assistant", Content: "Earlier answer"}) {
		t.Fatalf("prior message = %#v", history[1])
	}
	if history[2] != (llm.Message{Role: "user", Content: "Neue Frage"}) {
		t.Fatalf("new user message = %#v", history[2])
	}
}

func TestStreamMessageReturns503WhenLLMDependencyMissing(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"llm is not configured"`) {
		t.Fatalf("body = %q, want llm configuration error", rec.Body.String())
	}
}

func TestStreamMessageStillCompletesWhenTitleGenerationFails(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{titleErr: errors.New("title model unavailable")},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: assistant_message") {
		t.Fatalf("SSE body missing assistant message:\n%s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("SSE body missing done despite title failure:\n%s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("SSE body contains error for best-effort title failure:\n%s", body)
	}
}
