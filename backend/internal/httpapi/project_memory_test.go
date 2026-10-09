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

func TestRenderProjectContext(t *testing.T) {
	got := renderProjectContext(
		chat.Project{Name: "Amsterdam Trip", Description: "Family trip planning"},
		"Travel month: May",
	)
	for _, want := range []string{"Amsterdam Trip", "Family trip planning", "Travel month: May"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered context missing %q: %q", want, got)
		}
	}

	// No memory: still renders name, no dangling memory header.
	noMemory := renderProjectContext(chat.Project{Name: "Solo"}, "")
	if strings.Contains(noMemory, "Project memory") {
		t.Fatalf("empty memory should not render the memory header: %q", noMemory)
	}
}

// TestStreamMessageInjectsProjectMemory proves the end-to-end injection path: a
// chat that belongs to a project gets the project's name, description, and shared
// memory placed into the system message sent to the model.
func TestStreamMessageInjectsProjectMemory(t *testing.T) {
	projectID := "proj_1"
	var capturedHistory []llm.Message
	store := &fakeThreadStore{
		Thread:        chat.Thread{ID: "thr_1", UserID: testUser.ID, ProjectID: &projectID, Title: "Flights"},
		Project:       chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip", Description: "Family trip planning"},
		ProjectMemory: chat.ProjectMemory{ProjectID: projectID, Content: "Travel month: May"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{History: &capturedHistory},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"What should I pack?"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(capturedHistory) == 0 || capturedHistory[0].Role != "system" {
		t.Fatalf("history = %#v, want a leading system message", capturedHistory)
	}
	systemContent := capturedHistory[0].Content
	for _, want := range []string{"Amsterdam Trip", "Family trip planning", "Travel month: May"} {
		if !strings.Contains(systemContent, want) {
			t.Fatalf("system message missing %q:\n%s", want, systemContent)
		}
	}
}

// TestStreamMessageOmitsProjectContextForProjectlessThread guards that chats
// without a project get no injected context.
func TestStreamMessageOmitsProjectContextForProjectlessThread(t *testing.T) {
	var capturedHistory []llm.Message
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Loose chat"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{History: &capturedHistory},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(capturedHistory[0].Content, "belongs to a project") {
		t.Fatalf("projectless chat unexpectedly got project context:\n%s", capturedHistory[0].Content)
	}
}

// TestRefreshMemory_ProjectScopeGeneratesAndStores proves the generate→store path:
// the LLM-produced memory is persisted with the source message count.
func TestRefreshMemory_ProjectScopeGeneratesAndStores(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		Project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
	}
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "Travel month: May"}}

	err := s.refreshMemory(
		context.Background(),
		testUser,
		s.projectMemoryScope(testUser, store.Project),
		"",
		[]chat.Message{{Role: chat.RoleUser, Content: "When should we go?"}},
		7,
	)
	if err != nil {
		t.Fatalf("refreshMemory() error: %v", err)
	}
	if store.ProjectMemory.Content != "Travel month: May" {
		t.Fatalf("stored content = %q, want generated memory", store.ProjectMemory.Content)
	}
	if store.ProjectMemory.SourceMessageCount != 7 {
		t.Fatalf("stored source count = %d, want 7", store.ProjectMemory.SourceMessageCount)
	}
}

// TestRefreshProjectMemoryIfDue_NoNewMessagesIsNoOp guards the gate: with no new
// messages since the last refresh (zero delta), nothing regenerates.
func TestRefreshProjectMemoryIfDue_NoNewMessagesIsNoOp(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		Project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
		ProjectMessageCount: 0,
		Messages:            []chat.Message{{Role: chat.RoleUser, Content: "When?"}},
	}
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "must not be stored"}}

	if err := s.refreshProjectMemoryIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectMemoryIfDue() error: %v", err)
	}
	if store.ProjectMemory.Content != "" {
		t.Fatalf("memory = %q, want no refresh with zero new messages", store.ProjectMemory.Content)
	}
}

// TestRefreshProjectMemoryIfDue_AnyNewMessageRefreshes proves a single new message
// fires the gate and the incremental refresh folds in the recent (cross-thread)
// project messages.
func TestRefreshProjectMemoryIfDue_AnyNewMessageRefreshes(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		Project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
		ProjectMessageCount: 1,
		Messages:            []chat.Message{{Role: chat.RoleUser, Content: "Traveling in May"}},
	}
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "Travel month: May"}}

	if err := s.refreshProjectMemoryIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectMemoryIfDue() error: %v", err)
	}
	if store.ProjectMemory.Content != "Travel month: May" {
		t.Fatalf("memory = %q, want refreshed content", store.ProjectMemory.Content)
	}
	if store.ProjectMemory.SourceMessageCount != 1 {
		t.Fatalf("source count = %d, want 1", store.ProjectMemory.SourceMessageCount)
	}
}

// TestEditProjectMemory_AppliesAndReturns proves the project edit path stores
// and returns the LLM-applied memory, preserving the gate.
func TestEditProjectMemory_AppliesAndReturns(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		Project:       chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
		ProjectMemory: chat.ProjectMemory{ProjectID: projectID, Content: "- Travel month: May", SourceMessageCount: 5},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{EditedMemory: "- Travel month: June"},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects/proj_1/memory:edit", `{"instruction":"We moved the trip to June"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "June") {
		t.Fatalf("response missing edited memory:\n%s", rec.Body.String())
	}
	if store.ProjectMemory.Content != "- Travel month: June" {
		t.Fatalf("stored content = %q, want edited memory", store.ProjectMemory.Content)
	}
	if store.ProjectMemory.SourceMessageCount != 5 {
		t.Fatalf("source count = %d, want 5 (gate undisturbed)", store.ProjectMemory.SourceMessageCount)
	}
}

// TestEditProjectMemory_EmptyResultEmptiesMemory mirrors the user case: an empty
// LLM result is stored, preserving the gate.
func TestEditProjectMemory_EmptyResultEmptiesMemory(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		Project:       chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
		ProjectMemory: chat.ProjectMemory{ProjectID: projectID, Content: "- Old fact", SourceMessageCount: 6},
	}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{EditedMemory: ""}})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects/proj_1/memory:edit", `{"instruction":"Forget everything"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if store.ProjectMemory.Content != "" {
		t.Fatalf("stored content = %q, want emptied", store.ProjectMemory.Content)
	}
	if store.ProjectMemory.SourceMessageCount != 6 {
		t.Fatalf("source count = %d, want 6 (gate undisturbed)", store.ProjectMemory.SourceMessageCount)
	}
}

// TestEditProjectMemory_UnownedProjectIs404 guards the ownership path. The real
// store filters GetProject by user_id, so another user's project resolves to
// not-found; modelled here as a project whose id does not match the request. The
// edit must 404 and never upsert.
func TestEditProjectMemory_UnownedProjectIs404(t *testing.T) {
	store := &fakeThreadStore{
		Project: chat.Project{ID: "someone_elses", UserID: "other_user", Name: "Theirs"},
	}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{EditedMemory: "must not be stored"}})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects/proj_1/memory:edit", `{"instruction":"Remember X"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if store.ProjectMemory.Content != "" {
		t.Fatalf("memory upserted for an unowned project: %q", store.ProjectMemory.Content)
	}
}

func TestEditProjectMemory_EmptyInstructionIsBadRequest(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{Project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects/proj_1/memory:edit", `{"instruction":""}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestEditProjectMemory_NoLLMIsServiceUnavailable(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{Project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"}}
	srv := newAuthenticatedServer(t, Deps{Thread: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/projects/proj_1/memory:edit", `{"instruction":"We moved the trip to June"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestTranscriptFromMessages_SkipsNonChatRoles(t *testing.T) {
	transcript := transcriptFromMessages([]chat.Message{
		{Role: chat.RoleUser, Content: "When?"},
		{Role: chat.RoleTool, Content: "tool noise"},
		{Role: chat.RoleAssistant, Content: "May."},
		{Role: chat.RoleAssistant, Content: "  "},
	})
	if strings.Contains(transcript, "tool noise") {
		t.Fatalf("transcript should skip tool messages: %q", transcript)
	}
	if !strings.Contains(transcript, "User: When?") || !strings.Contains(transcript, "Assistant: May.") {
		t.Fatalf("transcript missing expected turns: %q", transcript)
	}
}
