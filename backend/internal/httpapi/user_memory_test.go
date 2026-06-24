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

// TestEditUserMemory_AppliesAndReturns proves the edit path: the LLM-applied
// memory is stored (preserving the source-message-count gate) and returned.
func TestEditUserMemory_AppliesAndReturns(t *testing.T) {
	store := &fakeThreadStore{userMemory: chat.UserMemory{Content: "- Works at Thoughtworks", SourceMessageCount: 3}}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{editedMemory: "- Works at Thoughtworks\n- Lives in Zurich"},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/me/memory:edit", `{"instruction":"Remember I live in Zurich"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Lives in Zurich") {
		t.Fatalf("response missing edited memory:\n%s", rec.Body.String())
	}
	if store.userMemory.Content != "- Works at Thoughtworks\n- Lives in Zurich" {
		t.Fatalf("stored content = %q, want edited memory", store.userMemory.Content)
	}
	if store.userMemory.SourceMessageCount != 3 {
		t.Fatalf("source count = %d, want 3 (gate undisturbed)", store.userMemory.SourceMessageCount)
	}
}

// TestEditUserMemory_EmptyResultEmptiesMemory guards the intended divergence from
// refreshMemory: an empty LLM result (the user emptied their memory) is stored as
// "" rather than skipped, and the source-message-count gate is preserved.
func TestEditUserMemory_EmptyResultEmptiesMemory(t *testing.T) {
	store := &fakeThreadStore{userMemory: chat.UserMemory{Content: "- Former baseball player", SourceMessageCount: 4}}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{editedMemory: ""},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/me/memory:edit", `{"instruction":"Forget everything about me"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.userMemory.Content != "" {
		t.Fatalf("stored content = %q, want emptied", store.userMemory.Content)
	}
	if store.userMemory.SourceMessageCount != 4 {
		t.Fatalf("source count = %d, want 4 (gate undisturbed)", store.userMemory.SourceMessageCount)
	}
}

func TestEditUserMemory_EmptyInstructionIsBadRequest(t *testing.T) {
	srv := newAuthenticatedServer(t, Deps{Thread: &fakeThreadStore{}, LLM: fakeChatClient{}})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/me/memory:edit", `{"instruction":"   "}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestEditUserMemory_NoLLMIsServiceUnavailable(t *testing.T) {
	srv := newAuthenticatedServer(t, Deps{Thread: &fakeThreadStore{}})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/me/memory:edit", `{"instruction":"Remember I live in Zurich"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

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
	store := &fakeThreadStore{
		thread:     chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Loose chat"},
		userMemory: chat.UserMemory{Content: "- Works at Acme in Zurich"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{history: &capturedHistory},
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
	store := &fakeThreadStore{
		userMessageCount: memoryRefreshThreshold - 1,
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "Hi"}},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "must not be stored"}}

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
	store := &fakeThreadStore{
		userMessageCount: memoryRefreshThreshold,
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "I moved to Zurich"}},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "- Lives in Zurich"}}

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
