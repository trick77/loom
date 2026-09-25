package httpapi

import (
	"context"
	"sync"
	"sync/atomic"
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
// honors the debounce: a project memory refreshed within the window is not
// regenerated even though the per-turn trigger fires every turn.
func TestRefreshProjectMemoryIfDue_DebounceSkipsFreshMemory(t *testing.T) {
	projectID := "proj_1"
	fresh := time.Now().Add(-5 * time.Minute) // < memoryProjectDebounce (15m)
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
		t.Fatalf("memory = %q, want unchanged (within the 15m debounce)", store.projectMemory.Content)
	}
}

// TestRefreshProjectMemoryIfDue_DebounceRefreshesStaleMemory proves a project
// memory older than the debounce regenerates.
func TestRefreshProjectMemoryIfDue_DebounceRefreshesStaleMemory(t *testing.T) {
	projectID := "proj_1"
	stale := time.Now().Add(-2 * time.Hour) // > memoryProjectDebounce (15m)
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
		t.Fatalf("memory = %q, want regenerated (older than the 15m debounce)", store.projectMemory.Content)
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

// Concurrent turns in one scope must not each start the same LLM refresh: the
// gate is check-then-act on stored counters, so without a guard every caller
// that reads the stale count regenerates. Only the first proceeds; the rest
// return at once and the next due check picks up the fresh counters.
func TestRefreshMemoryIfDue_SingleFlightPerScope(t *testing.T) {
	stale := time.Now().Add(-25 * time.Hour)
	store := &fakeThreadStore{
		userMessageCount: 50,
		userMemory:       chat.UserMemory{Content: "- prior", SourceMessageCount: 0, UpdatedAt: &stale},
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "I moved to Zurich"}},
	}
	entered := make(chan struct{}, 8)
	gate := make(chan struct{})
	var calls atomic.Int32
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "- REGENERATED", memoryEntered: entered, memoryGate: gate, memoryCalls: &calls}}
	refresh := func() error {
		return s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), memoryUserRefreshAge)
	}

	first := make(chan error, 1)
	go func() { first <- refresh() }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first refresh never reached the model")
	}

	// While the first refresh is inside the model call, four more turns fire.
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := refresh(); err != nil {
				t.Errorf("concurrent refreshMemoryIfDue() error: %v", err)
			}
		}()
	}
	wg.Wait()
	close(gate)
	if err := <-first; err != nil {
		t.Fatalf("first refreshMemoryIfDue() error: %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("GenerateMemory calls = %d, want 1", got)
	}
	if store.userMemory.Content != "- REGENERATED" {
		t.Fatalf("memory = %q, want regenerated once", store.userMemory.Content)
	}
}

// The gate compared the live message count to the count the memory was built
// from with "<=", so once threads were deleted and the count dropped, no
// refresh ran until the old number was exceeded again.
func TestRefreshMemoryIfDue_RefreshesWhenCountDropped(t *testing.T) {
	stale := time.Now().Add(-25 * time.Hour)
	store := &fakeThreadStore{
		userMessageCount: 6,
		userMemory:       chat.UserMemory{Content: "- prior", SourceMessageCount: 10, UpdatedAt: &stale},
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "still here"}},
	}
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "- REGENERATED"}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), memoryUserRefreshAge); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.userMemory.Content != "- REGENERATED" {
		t.Fatalf("memory = %q, want regenerated after the count dropped", store.userMemory.Content)
	}
	if store.userMemory.SourceMessageCount != 6 {
		t.Fatalf("SourceMessageCount = %d, want the current count 6", store.userMemory.SourceMessageCount)
	}
}

// After deletions the memory is rebuilt from what is left, not folded into
// the stale prior that may describe the deleted conversations; with nothing
// left it is cleared.
func TestRefreshMemoryIfDue_DeletionsRebuildWithoutThePrior(t *testing.T) {
	store := &fakeThreadStore{
		userMessageCount: 2,
		userMemory:       chat.UserMemory{Content: "- stale", SourceMessageCount: 60},
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	}
	priors := make(chan string, 1)
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "- fresh", memoryPriors: priors}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if got := <-priors; got != "" {
		t.Fatalf("GenerateMemory prior = %q, want the stale memory dropped", got)
	}
	if store.listLimit != 2 {
		t.Fatalf("list limit = %d, want the whole remaining transcript (2)", store.listLimit)
	}
	if store.userMemory.Content != "- fresh" || store.userMemory.SourceMessageCount != 2 {
		t.Fatalf("stored memory = %+v, want the rebuilt one over 2 messages", store.userMemory)
	}
}

func TestRefreshMemoryIfDue_EverythingDeletedClearsTheMemory(t *testing.T) {
	var calls atomic.Int32
	store := &fakeThreadStore{
		userMessageCount: 0,
		userMemory:       chat.UserMemory{Content: "- stale", SourceMessageCount: 60},
	}
	s := &server{thread: store, llm: fakeChatClient{memoryCalls: &calls}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.userMemory.Content != "" || store.userMemory.SourceMessageCount != 0 {
		t.Fatalf("stored memory = %+v, want cleared", store.userMemory)
	}
	if calls.Load() != 0 {
		t.Fatalf("GenerateMemory calls = %d, want 0 (nothing to summarise)", calls.Load())
	}
}

// A long history that no longer fits the rebuild window keeps its prior after
// a deletion: rebuilding from the newest messages alone would lose everything
// older than the window.
func TestRefreshMemoryIfDue_DeletionInLongHistoryKeepsThePrior(t *testing.T) {
	store := &fakeThreadStore{
		userMessageCount: memoryRebuildLimit + 50,
		userMemory:       chat.UserMemory{Content: "- long-term", SourceMessageCount: memoryRebuildLimit + 60},
		messages:         []chat.Message{{Role: chat.RoleUser, Content: "hi"}},
	}
	priors := make(chan string, 1)
	s := &server{thread: store, llm: fakeChatClient{projectMemory: "- folded", memoryPriors: priors}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.userMemoryScope(testUser), 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if got := <-priors; got != "- long-term" {
		t.Fatalf("GenerateMemory prior = %q, want the long-term memory kept", got)
	}
	if store.listLimit != memoryRebuildLimit {
		t.Fatalf("list limit = %d, want %d", store.listLimit, memoryRebuildLimit)
	}
}
