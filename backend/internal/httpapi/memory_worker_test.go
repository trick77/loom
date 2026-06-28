package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// TestRefreshMemoryIfDue_DebounceSkipsFreshMemory proves the staleness gate: a
// memory refreshed within minAge is left alone even when enough new messages have
// accumulated (the 24h user debounce / 1h project debounce).
func TestRefreshMemoryIfDue_DebounceSkipsFreshMemory(t *testing.T) {
	fresh := time.Now().Add(-30 * time.Minute)
	store := &fakeThreadStore{
		userMessageCount: 50, // far above the threshold
		userMemory:       chat.UserMemory{Content: "- prior", SourceMessageCount: 0, UpdatedAt: &fresh},
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "- REGENERATED"}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), memoryUserRefreshAge); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.userMemory.Content != "- prior" {
		t.Fatalf("memory = %q, want unchanged (refreshed too recently to be due)", store.userMemory.Content)
	}
}

// TestRefreshMemoryIfDue_DebounceRefreshesStaleMemory proves the complement: once
// the memory is older than minAge, an eligible scope regenerates.
func TestRefreshMemoryIfDue_DebounceRefreshesStaleMemory(t *testing.T) {
	stale := time.Now().Add(-25 * time.Hour)
	store := &fakeThreadStore{
		userMessageCount: 50,
		userMemory:       chat.UserMemory{Content: "- prior", SourceMessageCount: 0, UpdatedAt: &stale},
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "I moved to Zurich"}},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "- REGENERATED"}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), memoryUserRefreshAge); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.userMemory.Content != "- REGENERATED" {
		t.Fatalf("memory = %q, want regenerated (older than the debounce window)", store.userMemory.Content)
	}
}

// TestRefreshMemoryIfDue_AdaptiveWindowSizesToBacklog proves the fold window
// tracks the backlog (count - sourceCount) so messages are not skipped when
// refreshes are spaced out, capped at memoryRebuildLimit.
func TestRefreshMemoryIfDue_AdaptiveWindowSizesToBacklog(t *testing.T) {
	tests := []struct {
		name        string
		count       int
		sourceCount int
		wantLimit   int
	}{
		{name: "backlog under cap", count: 100, sourceCount: 0, wantLimit: 100},
		{name: "backlog over cap is capped", count: 500, sourceCount: 0, wantLimit: memoryRebuildLimit},
		{name: "small backlog", count: 2, sourceCount: 0, wantLimit: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeThreadStore{
				userMessageCount: tc.count,
				userMemory:       chat.UserMemory{Content: "- prior", SourceMessageCount: tc.sourceCount},
				messages:         []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
			}
			s := &server{thread: store, llm: fakeChatClient{projectMemory: "- ok"}}

			// minAge 0 disables the debounce so the window logic is exercised directly.
			if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), 0); err != nil {
				t.Fatalf("refreshMemoryIfDue() error: %v", err)
			}
			if store.listLimit != tc.wantLimit {
				t.Fatalf("list limit = %d, want %d", store.listLimit, tc.wantLimit)
			}
		})
	}
}

// TestRefreshProjectMemoryIfDue_DebounceSkipsFreshMemory proves the project path
// honors the 1h debounce: a project memory refreshed within the window is not
// regenerated even though the per-turn trigger fires every turn.
func TestRefreshProjectMemoryIfDue_DebounceSkipsFreshMemory(t *testing.T) {
	projectID := "proj_1"
	fresh := time.Now().Add(-30 * time.Minute) // < memoryProjectDebounce (1h)
	store := &fakeThreadStore{
		project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
		projectMessageCount: 50,
		projectMemory:       chat.ProjectMemory{ProjectID: projectID, Content: "Travel month: May", UpdatedAt: &fresh},
		messages:            []chat.Message{{Role: chat.RoleUser, Content: "Traveling soon"}},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "Travel month: June"}}

	if err := s.refreshProjectMemoryIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectMemoryIfDue() error: %v", err)
	}
	if store.projectMemory.Content != "Travel month: May" {
		t.Fatalf("memory = %q, want unchanged (within the 1h debounce)", store.projectMemory.Content)
	}
}

// TestRefreshProjectMemoryIfDue_DebounceRefreshesStaleMemory proves a project
// memory older than the 1h debounce regenerates.
func TestRefreshProjectMemoryIfDue_DebounceRefreshesStaleMemory(t *testing.T) {
	projectID := "proj_1"
	stale := time.Now().Add(-2 * time.Hour) // > memoryProjectDebounce (1h)
	store := &fakeThreadStore{
		project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
		projectMessageCount: 50,
		projectMemory:       chat.ProjectMemory{ProjectID: projectID, Content: "Travel month: May", UpdatedAt: &stale},
		messages:            []chat.Message{{Role: chat.RoleUser, Content: "Moved the trip to June"}},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "Travel month: June"}}

	if err := s.refreshProjectMemoryIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectMemoryIfDue() error: %v", err)
	}
	if store.projectMemory.Content != "Travel month: June" {
		t.Fatalf("memory = %q, want regenerated (older than the 1h debounce)", store.projectMemory.Content)
	}
}

// TestMemoryWorker_runOnce_RefreshesDueStaleScopes proves the batch backstop
// sweeps users and their projects, regenerating both the user memory (24h gate)
// and the project memory (1h gate) when each is due and stale.
func TestMemoryWorker_runOnce_RefreshesDueStaleScopes(t *testing.T) {
	projectID := "proj_1"
	staleUser := time.Now().Add(-25 * time.Hour)
	staleProject := time.Now().Add(-2 * time.Hour)
	store := &fakeThreadStore{
		project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Amsterdam Trip"},
		userMessageCount:    50,
		projectMessageCount: 50,
		userMemory:          chat.UserMemory{Content: "- old user", SourceMessageCount: 0, UpdatedAt: &staleUser},
		projectMemory:       chat.ProjectMemory{ProjectID: projectID, Content: "old project", UpdatedAt: &staleProject},
		messages:            []chat.Message{{Role: chat.RoleUser, Content: "fresh activity"}},
	}
	w := &MemoryWorker{s: &server{
		thread: store,
		users:  fakeUserStore{user: testUser, ok: true},
		llm:    fakeChatClient{projectMemory: "REGENERATED"},
	}}

	w.runOnce(context.Background())

	if store.userMemory.Content != "REGENERATED" {
		t.Fatalf("user memory = %q, want regenerated by the sweep", store.userMemory.Content)
	}
	if store.projectMemory.Content != "REGENERATED" {
		t.Fatalf("project memory = %q, want regenerated by the sweep", store.projectMemory.Content)
	}
}

// TestMemoryWorker_safely_RecoversPanic proves a panic in one scope is contained
// and does not prevent subsequent scopes from running — so a single bad scope
// can never kill the long-lived worker goroutine and silently stop all refreshes.
func TestMemoryWorker_safely_RecoversPanic(t *testing.T) {
	w := &MemoryWorker{s: &server{}}
	ranAfterPanic := false

	w.safely("boom", func() { panic("scope blew up") })
	w.safely("next", func() { ranAfterPanic = true })

	if !ranAfterPanic {
		t.Fatal("safely must continue running scopes after recovering a panic")
	}
}
