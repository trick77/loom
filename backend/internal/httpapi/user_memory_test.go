package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

func addOp(category, content string) []llm.FactOp {
	return []llm.FactOp{{Op: "add", Category: category, Content: content}}
}

func fact(id, category, content string, durable bool, lastSeen time.Time) chat.UserFact {
	return chat.UserFact{ID: id, Category: category, Content: content, Durable: durable, Origin: chat.FactOriginExtracted, FirstSeen: lastSeen, LastSeen: lastSeen}
}

func msg(id, thread, content string, at time.Time) chat.Message {
	return chat.Message{ID: id, ThreadID: thread, Role: chat.RoleUser, Content: content, CreatedAt: at}
}

func TestUserContextForUser_RendersBothBlocksIndependently(t *testing.T) {
	// Directives present but no facts: the directives block must still be injected.
	s := &server{thread: &fakeThreadStore{UserDirectives: []chat.UserDirective{{ID: "dir_0", Content: "Always use metric"}}}}
	out := s.userContextForUser(context.Background(), "alice")
	if !strings.Contains(out, "Standing instructions") || !strings.Contains(out, "Always use metric") || !strings.Contains(out, "dir_0") {
		t.Fatalf("directives block missing when there are no facts:\n%s", out)
	}
	if strings.Contains(out, "Personal context about the user") {
		t.Fatalf("facts block should be absent with no facts:\n%s", out)
	}

	// Facts present but no directives: only the facts block, ids included.
	s2 := &server{thread: &fakeThreadStore{Facts: []chat.UserFact{fact("f1", "work", "Backend dev", false, time.Now())}}}
	out2 := s2.userContextForUser(context.Background(), "alice")
	if !strings.Contains(out2, "Personal context about the user") || !strings.Contains(out2, "[f1] (work) Backend dev") {
		t.Fatalf("facts block missing:\n%s", out2)
	}
	if strings.Contains(out2, "Standing instructions") {
		t.Fatalf("directives block should be absent when there are none:\n%s", out2)
	}
}

func TestUserContextForUser_DirectivesOutrankFacts(t *testing.T) {
	s := &server{thread: &fakeThreadStore{
		UserDirectives: []chat.UserDirective{{ID: "dir_0", Content: "Be terse"}},
		Facts:          []chat.UserFact{fact("f1", "work", "Backend dev", false, time.Now())},
	}}
	out := s.userContextForUser(context.Background(), "alice")
	di := strings.Index(out, "Standing instructions")
	mi := strings.Index(out, "Personal context about the user")
	if di < 0 || mi < 0 || di > mi {
		t.Fatalf("directives block must come before the facts block (di=%d, mi=%d):\n%s", di, mi, out)
	}
}

func TestRenderUserFacts_BucketsByTime(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	out := renderUserFacts([]chat.UserFact{
		fact("e", "work", "Old project", false, now.Add(-400*day)),
		fact("t", "work", "Shipping v2", false, now.Add(-2*day)),
		fact("l", "personal", "Lives Zurich", true, now.Add(-400*day)),
		fact("r", "interest", "Started climbing", false, now.Add(-60*day)),
	}, now)
	order := []string{"## Long-term background", "Lives Zurich", "## Top of mind", "Shipping v2", "## Recent months", "Started climbing", "## Earlier context", "Old project"}
	last := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order:\n%s", want, out)
		}
		last = i
	}
	if renderUserFacts(nil, now) != "" {
		t.Fatalf("no facts must render nothing")
	}
}

func TestRenderUserFacts_CeilingDropsLeastRecentNonDurable(t *testing.T) {
	now := time.Now()
	long := strings.Repeat("x", 400)
	var facts []chat.UserFact
	for i := range 150 {
		facts = append(facts, fact("n"+strings.Repeat("0", 3)+string(rune('a'+i%26))+string(rune('a'+i/26)), "work", long, false, now.Add(-time.Duration(i)*time.Hour)))
	}
	facts = append(facts, fact("keep-durable", "personal", "Lives Zurich", true, now.Add(-9000*time.Hour)))
	out := renderUserFacts(facts, now)
	if len([]rune(out)) > userFactsRenderCeiling+2000 {
		t.Fatalf("rendered %d runes, want about the ceiling %d", len([]rune(out)), userFactsRenderCeiling)
	}
	if !strings.Contains(out, "Lives Zurich") {
		t.Fatalf("durable fact dropped by the ceiling")
	}
	if !strings.Contains(out, "[n000aa]") {
		t.Fatalf("most recent fact dropped by the ceiling")
	}
}

// TestStreamMessageInjectsUserFacts proves the facts are injected into every
// chat — here a projectless thread — via the system message.
func TestStreamMessageInjectsUserFacts(t *testing.T) {
	var capturedHistory []llm.Message
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Loose chat"},
		Facts:  []chat.UserFact{fact("f1", "work", "Works at Acme in Zurich", true, time.Now())},
	}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{History: &capturedHistory}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Where do I work?"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(capturedHistory) == 0 || !strings.Contains(capturedHistory[0].Content, "Works at Acme in Zurich") {
		t.Fatalf("system message missing the user's facts")
	}
}

func TestRefreshUserFacts_FoldsNewMessagesAndAdvancesCursor(t *testing.T) {
	at := time.Now().Add(-time.Hour)
	store := &fakeThreadStore{
		FactCursor:     &chat.UserFactCursor{},
		UserDirectives: []chat.UserDirective{{ID: "d", Content: "Always use metric"}},
		Messages:       []chat.Message{msg("m1", "thr_a", "I moved to Bern", at), msg("m2", "thr_b", "and adopted a cat", at.Add(time.Second))},
	}
	requests := make(chan fakeFactRequest, 1)
	s := &server{thread: store, llm: fakeChatClient{
		FactOps:      []llm.FactOp{{Op: "add", Category: "personal", Content: "Lives Bern", Threads: []string{"T1", "T9"}}},
		FactRequests: requests,
	}}

	if err := s.refreshUserFactsIfDue(context.Background(), testUser, memoryUserRefreshAge); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	req := <-requests
	if !strings.Contains(req.Transcript, "### T1") || !strings.Contains(req.Transcript, "### T2") || !strings.Contains(req.Transcript, "I moved to Bern") {
		t.Fatalf("transcript not grouped by thread label:\n%s", req.Transcript)
	}
	if !strings.Contains(req.Excluded, "Always use metric") {
		t.Fatalf("directives not passed as exclusions: %q", req.Excluded)
	}
	if len(store.FactBatches) != 1 {
		t.Fatalf("batches = %d, want 1", len(store.FactBatches))
	}
	b := store.FactBatches[0]
	if got := b.Ops[0].Threads; len(got) != 1 || got[0] != "thr_a" {
		t.Fatalf("op threads = %v, want T1 mapped to thr_a and the unknown T9 dropped", got)
	}
	if b.Cursor == nil || b.Cursor.LastMessageID != "m2" || !b.SeenAt.Equal(at.Add(time.Second)) {
		t.Fatalf("batch cursor/seen = %+v / %v, want the last message", b.Cursor, b.SeenAt)
	}
}

func TestRefreshUserFacts_DebouncedUnlessAFullBatchWaits(t *testing.T) {
	recent := time.Now().Add(-time.Hour)
	store := &fakeThreadStore{
		FactCursor: &chat.UserFactCursor{LastRefreshAt: &recent, LastConsolidatedAt: &recent},
		Messages:   []chat.Message{msg("m1", "thr_a", "hi", time.Now().Add(-10*time.Minute))},
	}
	s := &server{thread: store, llm: fakeChatClient{}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, memoryUserRefreshAge); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(store.FactBatches) != 0 {
		t.Fatalf("refreshed within the debounce window")
	}
}

func TestRefreshUserFacts_FailedExtractionKeepsCursor(t *testing.T) {
	store := &fakeThreadStore{
		FactCursor: &chat.UserFactCursor{LastMessageID: "m0"},
		Messages:   []chat.Message{msg("m1", "thr_a", "hi", time.Now().Add(-time.Hour))},
	}
	s := &server{thread: store, llm: fakeChatClient{FactErr: llm.ErrMemoryTruncated}}
	err := s.refreshUserFactsIfDue(context.Background(), testUser, 0)
	if !errors.Is(err, llm.ErrMemoryTruncated) || !isFactRefreshSkippable(err) {
		t.Fatalf("err = %v, want the truncation surfaced as skippable", err)
	}
	if len(store.FactBatches) != 0 || store.FactCursor.LastMessageID != "m0" {
		t.Fatalf("a failed extraction applied or moved the cursor")
	}
}

func TestRefreshUserFacts_ForgottenSentOnceThenPurged(t *testing.T) {
	store := &fakeThreadStore{
		FactCursor:     &chat.UserFactCursor{},
		ForgottenFacts: []string{"Works at Acme"},
		Messages:       []chat.Message{msg("m1", "thr_a", "forget that I work at Acme", time.Now().Add(-time.Hour))},
	}
	requests := make(chan fakeFactRequest, 1)
	s := &server{thread: store, llm: fakeChatClient{FactRequests: requests}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, 0); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if req := <-requests; len(req.Forgotten) != 1 || req.Forgotten[0] != "Works at Acme" {
		t.Fatalf("forgotten = %v, want the forgotten fact as do-not-re-add", req.Forgotten)
	}
	if !store.FactBatches[0].PurgeForgotten || len(store.ForgottenFacts) != 0 {
		t.Fatalf("forgotten facts not purged after the refresh saw them")
	}
}

func TestRefreshUserFacts_ConvertsOldMemoryFirst(t *testing.T) {
	blobAt := time.Date(2026, 10, 30, 22, 43, 0, 0, time.UTC)
	store := &fakeThreadStore{
		UserMemory: chat.UserMemory{Content: "## Work context\n- Backend dev at Acme", UpdatedAt: &blobAt},
		Messages: []chat.Message{
			msg("old", "thr_a", "already in the memory text", blobAt.Add(-time.Hour)),
			msg("new", "thr_a", "I moved to Bern", blobAt.Add(time.Hour)),
		},
	}
	requests := make(chan fakeFactRequest, 1)
	s := &server{thread: store, llm: fakeChatClient{
		ConvertedOps: []llm.FactOp{{Op: "add", Category: "work", Content: "Backend dev at Acme", Durable: true}, {Op: "retire", ID: "x"}},
		FactRequests: requests,
	}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, memoryUserRefreshAge); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	conv := store.FactBatches[0]
	if conv.Origin != chat.FactOriginConverted || conv.Ops[1].Kind != "" || !conv.SeenAt.Equal(blobAt) {
		t.Fatalf("conversion batch = %+v, want converted adds only, dated at the memory's refresh", conv)
	}
	if req := <-requests; strings.Contains(req.Transcript, "already in the memory text") || !strings.Contains(req.Transcript, "moved to Bern") {
		t.Fatalf("after conversion the refresh must read only messages newer than the memory text:\n%s", req.Transcript)
	}
}

func TestRefreshUserFacts_ConsolidatesWeekly(t *testing.T) {
	old := time.Now().Add(-8 * 24 * time.Hour)
	var facts []chat.UserFact
	for i := range userFactsConsolidateMin {
		facts = append(facts, fact(string(rune('a'+i)), "work", "fact", false, time.Now()))
	}
	store := &fakeThreadStore{FactCursor: &chat.UserFactCursor{LastConsolidatedAt: &old}, Facts: facts}
	s := &server{thread: store, llm: fakeChatClient{FactMerges: []llm.FactMerge{{IDs: []string{"a", "b"}, Content: "merged"}}}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, 0); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(store.Merges) != 1 || store.FactCursor.LastConsolidatedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("merges = %+v, cursor = %+v; want one merge and the pass stamped", store.Merges, store.FactCursor)
	}
}

func TestFactsTranscript_BudgetAndToolMessages(t *testing.T) {
	at := time.Now()
	big := strings.Repeat("y", userFactsMessageBudget+100)
	var messages []chat.Message
	messages = append(messages, chat.Message{ID: "tool", ThreadID: "a", Role: chat.RoleTool, Content: "x", CreatedAt: at})
	for i := range 60 {
		messages = append(messages, msg(string(rune('A'+i)), "a", big, at))
	}
	transcript, labels, used := factsTranscript(messages)
	if used >= len(messages) || used < 2 {
		t.Fatalf("used = %d of %d, want the budget to stop the batch early", used, len(messages))
	}
	if len([]rune(transcript)) > userFactsTranscriptBudget+200 {
		t.Fatalf("transcript %d runes, over the budget", len([]rune(transcript)))
	}
	if labels["T1"] != "a" || strings.Contains(transcript, strings.Repeat("y", userFactsMessageBudget+1)) {
		t.Fatalf("labels %v / per-message cap not applied", labels)
	}
}

func TestHandleGetUserFacts(t *testing.T) {
	store := &fakeThreadStore{Facts: []chat.UserFact{
		fact("t", "work", "Shipping v2", false, time.Now()),
		fact("l", "personal", "Lives Zurich", true, time.Now().Add(-9000*time.Hour)),
	}}
	srv := newAuthenticatedServer(t, Deps{Thread: store})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/me/facts", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got []struct {
		ID      string `json:"id"`
		Bucket  string `json:"bucket"`
		Durable bool   `json:"durable"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 || got[0].ID != "l" || got[0].Bucket != "long_term" || got[1].Bucket != "top_of_mind" {
		t.Fatalf("facts = %+v, want long-term first, each with its bucket", got)
	}
}

func TestUserFactsWriteEndpointsDoNotExist(t *testing.T) {
	srv := newAuthenticatedServer(t, Deps{Thread: &fakeThreadStore{}})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(method, "/api/me/facts", ""))
		if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
			t.Fatalf("%s /api/me/facts = %d, want no write endpoint", method, rec.Code)
		}
	}
}
