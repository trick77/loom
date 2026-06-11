package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/llm"
)

func TestRenderUserContext(t *testing.T) {
	got := renderUserContext("- Works at Acme\n- Lives in Zurich")
	for _, want := range []string{"Works at Acme", "Lives in Zurich"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered context missing %q: %q", want, got)
		}
	}
	// No memory: nothing is injected.
	if empty := renderUserContext("  "); empty != "" {
		t.Fatalf("empty memory should render nothing, got %q", empty)
	}
}

// TestStreamMessageInjectsUserMemory proves user memory is injected into every
// chat — here a projectless thread — via the system message.
func TestStreamMessageInjectsUserMemory(t *testing.T) {
	var capturedHistory []llm.Message
	store := &fakeChatStore{
		thread:     chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Loose chat"},
		userMemory: chat.UserMemory{Content: "- Works at Acme in Zurich"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{history: &capturedHistory},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Where do I work?"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(capturedHistory) == 0 || capturedHistory[0].Role != "system" {
		t.Fatalf("history = %#v, want a leading system message", capturedHistory)
	}
	if !strings.Contains(capturedHistory[0].Content, "Works at Acme in Zurich") {
		t.Fatalf("system message missing user memory:\n%s", capturedHistory[0].Content)
	}
}

// TestRefreshUserMemoryIfDue_BelowThresholdIsNoOp guards the gate: too few new
// messages must not trigger a refresh.
func TestRefreshUserMemoryIfDue_BelowThresholdIsNoOp(t *testing.T) {
	store := &fakeChatStore{
		userMessageCount: memoryRefreshThreshold - 1,
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "Hi"}},
	}
	s := &server{chat: store, llm: fakeChatClient{projectMemory: "must not be stored"}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser)); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.userMemory.Content != "" {
		t.Fatalf("memory = %q, want no refresh below the gate", store.userMemory.Content)
	}
}

// TestRefreshUserMemoryIfDue_AtThresholdRefreshes proves the gate fires and the
// incremental refresh folds in the recent messages across all threads.
func TestRefreshUserMemoryIfDue_AtThresholdRefreshes(t *testing.T) {
	store := &fakeChatStore{
		userMessageCount: memoryRefreshThreshold,
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "I moved to Zurich"}},
	}
	s := &server{chat: store, llm: fakeChatClient{projectMemory: "- Lives in Zurich"}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser)); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.userMemory.Content != "- Lives in Zurich" {
		t.Fatalf("memory = %q, want refreshed content", store.userMemory.Content)
	}
	if store.userMemory.SourceMessageCount != memoryRefreshThreshold {
		t.Fatalf("source count = %d, want %d", store.userMemory.SourceMessageCount, memoryRefreshThreshold)
	}
}
