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

func TestUserContextForUser_RendersBothBlocksIndependently(t *testing.T) {
	// Directives present but derived memory empty: the directives block must still
	// be injected (the old code returned "" whenever memory was blank).
	fake := &fakeThreadStore{
		UserDirectives: []chat.UserDirective{{ID: "dir_0", Content: "Always use metric"}},
	}
	s := &server{thread: fake}
	out := s.userContextForUser(context.Background(), "alice")
	if !strings.Contains(out, "Standing instructions") || !strings.Contains(out, "Always use metric") {
		t.Fatalf("directives block missing when memory is empty:\n%s", out)
	}
	if !strings.Contains(out, "dir_0") {
		t.Fatalf("directive id should be injected so the model can edit it:\n%s", out)
	}
	if strings.Contains(out, "Personal context about the user") {
		t.Fatalf("derived block should be absent when memory is empty:\n%s", out)
	}

	// Derived memory present but no directives: only the derived block.
	fake2 := &fakeThreadStore{UserMemory: chat.UserMemory{Content: "## Work context\n- Backend dev"}}
	s2 := &server{thread: fake2}
	out2 := s2.userContextForUser(context.Background(), "alice")
	if !strings.Contains(out2, "Personal context about the user") || !strings.Contains(out2, "Backend dev") {
		t.Fatalf("derived block missing:\n%s", out2)
	}
	if strings.Contains(out2, "Standing instructions") {
		t.Fatalf("directives block should be absent when there are none:\n%s", out2)
	}
}

func TestUserMemoryScope_ExclusionsFeedDirectiveContent(t *testing.T) {
	// The dedup source: the user scope must surface directive CONTENT (no ids) so
	// the generator can avoid restating a standing instruction in derived memory.
	fake := &fakeThreadStore{
		UserDirectives: []chat.UserDirective{
			{ID: "dir_0", Content: "Always use metric"},
			{ID: "dir_1", Content: "Call me Jan"},
		},
	}
	s := &server{thread: fake}
	scope := s.userMemoryScope(testUser)
	if scope.exclusions == nil {
		t.Fatal("user scope must set an exclusions hook")
	}
	out, err := scope.exclusions(context.Background())
	if err != nil {
		t.Fatalf("exclusions() error: %v", err)
	}
	if !strings.Contains(out, "Always use metric") || !strings.Contains(out, "Call me Jan") {
		t.Fatalf("exclusions missing directive content:\n%s", out)
	}
	if strings.Contains(out, "dir_0") {
		t.Fatalf("exclusions should carry content only, not ids:\n%s", out)
	}
}

func TestProjectMemoryScope_HasNoExclusions(t *testing.T) {
	// Project memory must be unaffected by the dedup plumbing.
	s := &server{thread: &fakeThreadStore{}}
	scope := s.projectMemoryScope(testUser, chat.Project{ID: "p1", Name: "P"})
	if scope.exclusions != nil {
		t.Fatal("project scope must not set an exclusions hook")
	}
}

func TestUserContextForUser_DirectivesOutrankDerived(t *testing.T) {
	fake := &fakeThreadStore{
		UserDirectives: []chat.UserDirective{{ID: "dir_0", Content: "Be terse"}},
		UserMemory:     chat.UserMemory{Content: "## Work context\n- Backend dev"},
	}
	s := &server{thread: fake}
	out := s.userContextForUser(context.Background(), "alice")
	di := strings.Index(out, "Standing instructions")
	mi := strings.Index(out, "Personal context about the user")
	if di < 0 || mi < 0 || di > mi {
		t.Fatalf("directives block must come before the derived block (di=%d, mi=%d):\n%s", di, mi, out)
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
		Thread:     chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Loose chat"},
		UserMemory: chat.UserMemory{Content: "- Works at Acme in Zurich"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{History: &capturedHistory},
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

// TestRefreshUserMemoryIfDue_NoNewMessagesIsNoOp guards the gate: with no new
// messages since the last refresh (zero delta), nothing regenerates.
func TestRefreshUserMemoryIfDue_NoNewMessagesIsNoOp(t *testing.T) {
	store := &fakeThreadStore{
		UserMessageCount: 0,
		Messages:         []chat.Message{{Role: chat.RoleUser, Content: "Hi"}},
	}
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "must not be stored"}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.UserMemory.Content != "" {
		t.Fatalf("memory = %q, want no refresh with zero new messages", store.UserMemory.Content)
	}
}

// TestRefreshUserMemoryIfDue_NegativeDeltaRefoldsRemainingTranscript guards
// the deletion case: when messages were deleted since the last refresh (count
// < sourceCount) there is no "new since last time" window, so the remaining
// transcript is folded in full — never a negative window, and never a memory
// left describing conversations that no longer exist.
func TestRefreshUserMemoryIfDue_NegativeDeltaRefoldsRemainingTranscript(t *testing.T) {
	store := &fakeThreadStore{
		UserMessageCount: 2,
		UserMemory:       chat.UserMemory{Content: "- prior", SourceMessageCount: 5},
		Messages:         []chat.Message{{Role: chat.RoleUser, Content: "Hi"}},
	}
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "- rebuilt"}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.UserMemory.Content != "- rebuilt" {
		t.Fatalf("memory = %q, want rebuilt from the remaining transcript", store.UserMemory.Content)
	}
	if store.UserMemory.SourceMessageCount != 2 {
		t.Fatalf("SourceMessageCount = %d, want the current count 2", store.UserMemory.SourceMessageCount)
	}
}

// TestRefreshUserMemoryIfDue_AnyNewMessageRefreshes proves a single new message
// fires the gate and the incremental refresh folds in the recent messages.
func TestRefreshUserMemoryIfDue_AnyNewMessageRefreshes(t *testing.T) {
	store := &fakeThreadStore{
		UserMessageCount: 1,
		Messages:         []chat.Message{{Role: chat.RoleUser, Content: "I moved to Zurich"}},
	}
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "- Lives in Zurich"}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.UserMemory.Content != "- Lives in Zurich" {
		t.Fatalf("memory = %q, want refreshed content", store.UserMemory.Content)
	}
	if store.UserMemory.SourceMessageCount != 1 {
		t.Fatalf("source count = %d, want 1", store.UserMemory.SourceMessageCount)
	}
}
