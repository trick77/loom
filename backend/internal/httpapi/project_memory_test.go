package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

func TestBuildLLMHistory_InjectsProjectContextOnlyWhenSet(t *testing.T) {
	user := auth.User{ID: "u1", ResponseLanguage: "auto"}
	newMsg := chat.Message{Role: chat.RoleUser, Content: "Hi"}

	without := buildLLMHistory(user, "", "", "", "", "", nil, newMsg)
	if without[0].Role != "system" {
		t.Fatalf("first message role = %q, want system", without[0].Role)
	}
	if strings.Contains(without[0].Content, "Project") {
		t.Fatalf("system prompt unexpectedly contains project context: %q", without[0].Content)
	}

	with := buildLLMHistory(user, "", "", "Project name: Amsterdam Trip", "", "", nil, newMsg)
	if !strings.Contains(with[0].Content, "Project name: Amsterdam Trip") {
		t.Fatalf("system prompt missing project context: %q", with[0].Content)
	}
	// The base system prompt is preserved alongside the injected context.
	if !strings.HasPrefix(with[0].Content, without[0].Content) {
		t.Fatalf("project context did not append to base system prompt: %q", with[0].Content)
	}
}

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
		thread:        chat.Thread{ID: "thr_1", UserID: testUser.ID, ProjectID: &projectID, Title: "Flights"},
		project:       chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip", Description: "Family trip planning"},
		projectMemory: chat.ProjectMemory{ProjectID: projectID, Content: "Travel month: May"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{history: &capturedHistory},
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

func TestStreamMessageAutoFillsEmptyProjectDescriptionAfterTwoTurns(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		thread:  chat.Thread{ID: "thr_1", UserID: testUser.ID, ProjectID: &projectID, Title: "Planning"},
		project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "Research", Description: ""},
		messages: []chat.Message{
			{ID: "msg_1", ThreadID: "thr_1", Role: chat.RoleUser, Content: "Collect the papers."},
			{ID: "msg_2", ThreadID: "thr_1", Role: chat.RoleAssistant, Content: "I will track the reading list."},
		},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{projectDescription: "Tracks paper research and reading priorities."},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Add comparison notes."}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: project") {
		t.Fatalf("stream missing project event:\n%s", body)
	}
	if !strings.Contains(body, `"description":"Tracks paper research and reading priorities."`) {
		t.Fatalf("stream missing generated description:\n%s", body)
	}
	if !store.projectDescriptionChanged {
		t.Fatal("project description was not persisted")
	}
}

func TestStreamMessageDoesNotAutoFillProjectDescriptionBeforeTwoTurns(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		thread:  chat.Thread{ID: "thr_1", UserID: testUser.ID, ProjectID: &projectID, Title: "Planning"},
		project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "Research", Description: ""},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{projectDescription: "Must not be used."},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Start notes."}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "event: project") {
		t.Fatalf("stream unexpectedly emitted project event:\n%s", rec.Body.String())
	}
	if store.projectDescriptionChanged {
		t.Fatal("project description changed before threshold")
	}
}

// TestStreamMessageOmitsProjectContextForProjectlessThread guards that chats
// without a project get no injected context.
func TestStreamMessageOmitsProjectContextForProjectlessThread(t *testing.T) {
	var capturedHistory []llm.Message
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Loose chat"},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: store,
		LLM:    fakeChatClient{history: &capturedHistory},
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

// TestRefreshProjectMemory_GeneratesAndStores proves the generate→store path:
// the LLM-produced memory is persisted with the source message count.
func TestRefreshProjectMemory_GeneratesAndStores(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		project: chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "Travel month: May"}}

	err := s.refreshProjectMemory(
		context.Background(),
		testUser,
		projectID,
		"",
		[]chat.Message{{Role: chat.RoleUser, Content: "When should we go?"}},
		7,
	)
	if err != nil {
		t.Fatalf("refreshProjectMemory() error: %v", err)
	}
	if store.projectMemory.Content != "Travel month: May" {
		t.Fatalf("stored content = %q, want generated memory", store.projectMemory.Content)
	}
	if store.projectMemory.SourceMessageCount != 7 {
		t.Fatalf("stored source count = %d, want 7", store.projectMemory.SourceMessageCount)
	}
}

// TestRefreshProjectMemoryIfDue_BelowThresholdIsNoOp guards the gate: too few
// new messages must not trigger a refresh.
func TestRefreshProjectMemoryIfDue_BelowThresholdIsNoOp(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
		projectMessageCount: memoryRefreshThreshold - 1,
		messages:            []chat.Message{{Role: chat.RoleUser, Content: "When?"}},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "must not be stored"}}

	if err := s.refreshProjectMemoryIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectMemoryIfDue() error: %v", err)
	}
	if store.projectMemory.Content != "" {
		t.Fatalf("memory = %q, want no refresh below the gate", store.projectMemory.Content)
	}
}

// TestRefreshProjectMemoryIfDue_AtThresholdRefreshes proves the gate fires and
// the incremental refresh folds in the recent (cross-thread) project messages.
func TestRefreshProjectMemoryIfDue_AtThresholdRefreshes(t *testing.T) {
	projectID := "proj_1"
	store := &fakeThreadStore{
		project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
		projectMessageCount: memoryRefreshThreshold,
		messages:            []chat.Message{{Role: chat.RoleUser, Content: "Traveling in May"}},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "Travel month: May"}}

	if err := s.refreshProjectMemoryIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectMemoryIfDue() error: %v", err)
	}
	if store.projectMemory.Content != "Travel month: May" {
		t.Fatalf("memory = %q, want refreshed content", store.projectMemory.Content)
	}
	if store.projectMemory.SourceMessageCount != memoryRefreshThreshold {
		t.Fatalf("source count = %d, want %d", store.projectMemory.SourceMessageCount, memoryRefreshThreshold)
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
