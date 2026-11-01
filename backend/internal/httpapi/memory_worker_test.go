package httpapi

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// projectStore is a fake holding one project with the given memory and count.
func projectStore(memory chat.ProjectMemory, count int, messages ...chat.Message) *fakeThreadStore {
	memory.ProjectID = "proj_1"
	return &fakeThreadStore{
		Project:             chat.Project{ID: "proj_1", UserID: testUser.ID, Name: "Amsterdam Trip"},
		ProjectMemory:       memory,
		ProjectMessageCount: count,
		Messages:            messages,
	}
}

// An edit while a refresh of the same memory is in flight is refused: the
// refresh read the memory before the edit and would write that stale prior
// back over it, silently undoing what the user asked to forget.
func TestEditMemory_RefusedWhileARefreshRuns(t *testing.T) {
	store := projectStore(chat.ProjectMemory{Content: "- X"}, 0)
	s := &server{thread: store, llm: fakeChatClient{EditedMemory: "- (X forgotten)"}}
	scope := s.projectMemoryScope(testUser, store.Project)
	release, _ := s.inflight.tryAcquire("memory:" + scope.key)
	defer release()

	if err := s.editMemory(context.Background(), testUser, scope, "forget X"); !errors.Is(err, errMemoryBusy) {
		t.Fatalf("editMemory() error = %v, want errMemoryBusy", err)
	}
	if store.ProjectMemory.Content != "- X" {
		t.Fatalf("memory = %q, want untouched while the refresh holds it", store.ProjectMemory.Content)
	}
	// A second refresh that would otherwise be due skips instead of racing it.
	store.ProjectMessageCount = 5
	store.Messages = []chat.Message{{Role: chat.RoleUser, Content: "hi"}}
	if err := s.refreshMemoryIfDue(context.Background(), testUser, scope, 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error = %v, want nil (skipped)", err)
	}
	if store.ProjectMemory.Content != "- X" {
		t.Fatalf("memory = %q, want untouched by a refresh that found the lock held", store.ProjectMemory.Content)
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
		{name: "backlog over cap is capped", count: memoryRebuildLimit + 100, sourceCount: 0, wantLimit: memoryRebuildLimit},
		{name: "small backlog", count: 2, sourceCount: 0, wantLimit: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := projectStore(chat.ProjectMemory{Content: "- prior", SourceMessageCount: tc.sourceCount}, tc.count,
				chat.Message{Role: chat.RoleUser, Content: "hi"})
			s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "- ok"}}

			// minAge 0 disables the debounce so the window logic is exercised directly.
			if err := s.refreshMemoryIfDue(context.Background(), testUser, s.projectMemoryScope(testUser, store.Project), 0); err != nil {
				t.Fatalf("refreshMemoryIfDue() error: %v", err)
			}
			if store.ListLimit != tc.wantLimit {
				t.Fatalf("list limit = %d, want %d", store.ListLimit, tc.wantLimit)
			}
		})
	}
}

// TestRefreshProjectMemoryIfDue_DebounceSkipsFreshMemory proves the project path
// honors the debounce: a project memory refreshed within the window is not
// regenerated even though the per-turn trigger fires every turn.
func TestRefreshProjectMemoryIfDue_DebounceSkipsFreshMemory(t *testing.T) {
	fresh := time.Now().Add(-5 * time.Minute) // < memoryProjectDebounce (15m)
	store := projectStore(chat.ProjectMemory{Content: "Travel month: May", UpdatedAt: &fresh}, 50,
		chat.Message{Role: chat.RoleUser, Content: "Traveling soon"})
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "Travel month: June"}}

	if err := s.refreshProjectMemoryIfDue(context.Background(), testUser, "proj_1"); err != nil {
		t.Fatalf("refreshProjectMemoryIfDue() error: %v", err)
	}
	if store.ProjectMemory.Content != "Travel month: May" {
		t.Fatalf("memory = %q, want unchanged (within the 15m debounce)", store.ProjectMemory.Content)
	}
}

// TestRefreshProjectMemoryIfDue_DebounceRefreshesStaleMemory proves a project
// memory older than the debounce regenerates.
func TestRefreshProjectMemoryIfDue_DebounceRefreshesStaleMemory(t *testing.T) {
	stale := time.Now().Add(-2 * time.Hour) // > memoryProjectDebounce (15m)
	store := projectStore(chat.ProjectMemory{Content: "Travel month: May", UpdatedAt: &stale}, 50,
		chat.Message{Role: chat.RoleUser, Content: "Moved the trip to June"})
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "Travel month: June"}}

	if err := s.refreshProjectMemoryIfDue(context.Background(), testUser, "proj_1"); err != nil {
		t.Fatalf("refreshProjectMemoryIfDue() error: %v", err)
	}
	if store.ProjectMemory.Content != "Travel month: June" {
		t.Fatalf("memory = %q, want regenerated (older than the 15m debounce)", store.ProjectMemory.Content)
	}
}

// TestMemoryWorker_runOnce_RefreshesDueStaleScopes proves the batch backstop
// sweeps users and their projects: the user's new messages become facts and
// the stale project memory regenerates.
func TestMemoryWorker_runOnce_RefreshesDueStaleScopes(t *testing.T) {
	staleProject := time.Now().Add(-2 * time.Hour)
	store := projectStore(chat.ProjectMemory{Content: "old project", UpdatedAt: &staleProject}, 50,
		chat.Message{ID: "m1", ThreadID: "thr_1", Role: chat.RoleUser, Content: "I moved to Zurich", CreatedAt: time.Now().Add(-time.Hour)})
	w := &MemoryWorker{s: &server{
		thread: store,
		users:  fakeUserStore{user: testUser, ok: true},
		llm:    fakeChatClient{ProjectMemory: "REGENERATED", FactOps: addOp("personal", "Lives Zurich")},
	}}

	w.runOnce(context.Background())

	if len(store.Facts) != 1 || store.Facts[0].Content != "Lives Zurich" {
		t.Fatalf("facts = %+v, want the new message folded in by the sweep", store.Facts)
	}
	if store.ProjectMemory.Content != "REGENERATED" {
		t.Fatalf("project memory = %q, want regenerated by the sweep", store.ProjectMemory.Content)
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
	store := projectStore(chat.ProjectMemory{Content: "- prior", UpdatedAt: &stale}, 50,
		chat.Message{Role: chat.RoleUser, Content: "Moved the trip to June"})
	entered := make(chan struct{}, 8)
	gate := make(chan struct{})
	var calls atomic.Int32
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "- REGENERATED", MemoryEntered: entered, MemoryGate: gate, MemoryCalls: &calls}}
	refresh := func() error {
		return s.refreshMemoryIfDue(context.Background(), testUser, s.projectMemoryScope(testUser, store.Project), memoryProjectDebounce)
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
	if store.ProjectMemory.Content != "- REGENERATED" {
		t.Fatalf("memory = %q, want regenerated once", store.ProjectMemory.Content)
	}
}

// The gate compared the live message count to the count the memory was built
// from with "<=", so once threads were deleted and the count dropped, no
// refresh ran until the old number was exceeded again.
func TestRefreshMemoryIfDue_RefreshesWhenCountDropped(t *testing.T) {
	stale := time.Now().Add(-25 * time.Hour)
	store := projectStore(chat.ProjectMemory{Content: "- prior", SourceMessageCount: 10, UpdatedAt: &stale}, 6,
		chat.Message{Role: chat.RoleUser, Content: "still here"})
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "- REGENERATED"}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.projectMemoryScope(testUser, store.Project), memoryProjectDebounce); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.ProjectMemory.Content != "- REGENERATED" {
		t.Fatalf("memory = %q, want regenerated after the count dropped", store.ProjectMemory.Content)
	}
	if store.ProjectMemory.SourceMessageCount != 6 {
		t.Fatalf("SourceMessageCount = %d, want the current count 6", store.ProjectMemory.SourceMessageCount)
	}
}

// After deletions the memory is rebuilt from what is left, not folded into
// the stale prior that may describe the deleted conversations; with nothing
// left it is cleared.
func TestRefreshMemoryIfDue_DeletionsRebuildWithoutThePrior(t *testing.T) {
	store := projectStore(chat.ProjectMemory{Content: "- stale", SourceMessageCount: 60}, 2,
		chat.Message{Role: chat.RoleUser, Content: "hi"})
	priors := make(chan string, 1)
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "- fresh", MemoryPriors: priors}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.projectMemoryScope(testUser, store.Project), 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if got := <-priors; got != "" {
		t.Fatalf("GenerateMemory prior = %q, want the stale memory dropped", got)
	}
	if store.ListLimit != 2 {
		t.Fatalf("list limit = %d, want the whole remaining transcript (2)", store.ListLimit)
	}
	if store.ProjectMemory.Content != "- fresh" || store.ProjectMemory.SourceMessageCount != 2 {
		t.Fatalf("stored memory = %+v, want the rebuilt one over 2 messages", store.ProjectMemory)
	}
}

func TestRefreshMemoryIfDue_EverythingDeletedClearsTheMemory(t *testing.T) {
	var calls atomic.Int32
	store := projectStore(chat.ProjectMemory{Content: "- stale", SourceMessageCount: 60}, 0)
	s := &server{thread: store, llm: fakeChatClient{MemoryCalls: &calls}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.projectMemoryScope(testUser, store.Project), 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if store.ProjectMemory.Content != "" || store.ProjectMemory.SourceMessageCount != 0 {
		t.Fatalf("stored memory = %+v, want cleared", store.ProjectMemory)
	}
	if calls.Load() != 0 {
		t.Fatalf("GenerateMemory calls = %d, want 0 (nothing to summarise)", calls.Load())
	}
}

// A long history that no longer fits the rebuild window keeps its prior after
// a deletion: rebuilding from the newest messages alone would lose everything
// older than the window.
func TestRefreshMemoryIfDue_DeletionInLongHistoryKeepsThePrior(t *testing.T) {
	store := projectStore(chat.ProjectMemory{Content: "- long-term", SourceMessageCount: memoryRebuildLimit + 60}, memoryRebuildLimit+50,
		chat.Message{Role: chat.RoleUser, Content: "hi"})
	priors := make(chan string, 1)
	s := &server{thread: store, llm: fakeChatClient{ProjectMemory: "- folded", MemoryPriors: priors}}

	if err := s.refreshMemoryIfDue(context.Background(), testUser, s.projectMemoryScope(testUser, store.Project), 0); err != nil {
		t.Fatalf("refreshMemoryIfDue() error: %v", err)
	}
	if got := <-priors; got != "- long-term" {
		t.Fatalf("GenerateMemory prior = %q, want the long-term memory kept", got)
	}
	if store.ListLimit != memoryRebuildLimit {
		t.Fatalf("list limit = %d, want %d", store.ListLimit, memoryRebuildLimit)
	}
}
