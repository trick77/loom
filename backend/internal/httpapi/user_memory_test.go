package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
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

// Over the ceiling, recency decides what is left out, not category order:
// a 3-year-old work fact goes before a 7-month-old interest fact.
func TestFitRenderCeiling_DropsByLastSeenAcrossCategories(t *testing.T) {
	now := time.Now()
	day := 24 * time.Hour
	filler := strings.Repeat("x", userFactsRenderCeiling-200)
	sorted := []chat.UserFact{
		fact("big", "personal", filler, false, now),
		fact("work-old", "work", strings.Repeat("w", 150), false, now.Add(-1100*day)),
		fact("interest-newer", "interest", strings.Repeat("i", 150), false, now.Add(-210*day)),
	}
	chat.SortUserFacts(sorted, now)
	kept := fitRenderCeiling(sorted)
	ids := make([]string, len(kept))
	for i, f := range kept {
		ids[i] = f.ID
	}
	if slices.Contains(ids, "work-old") || !slices.Contains(ids, "interest-newer") {
		t.Fatalf("kept %v, want the least recently seen fact dropped first", ids)
	}
}

// The collapse guard halves at most the size the prompt asks for: an
// oversized prior condensed to target size is accepted, a fragment is not.
func TestRefreshMemory_CondensingAnOversizedPriorIsAccepted(t *testing.T) {
	prior := strings.Repeat("- fact line\n", 250) // ~3000 chars, over the prompt's 2000 target
	for _, tc := range []struct {
		result string
		ok     bool
	}{{strings.Repeat("y", 1400), true}, {strings.Repeat("y", 300), false}} {
		store := projectStore(chat.ProjectMemory{Content: prior}, 0)
		s := &server{thread: store, llm: fakeChatClient{ProjectMemory: tc.result}}
		err := s.refreshMemory(context.Background(), testUser, s.projectMemoryScope(testUser, store.Project),
			prior, []chat.Message{{Role: chat.RoleUser, Content: "hi"}}, 7, false)
		if (err == nil) != tc.ok {
			t.Fatalf("result of %d chars: err = %v, want accepted=%v", len(tc.result), err, tc.ok)
		}
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

// A transient failure keeps the cursor: the next sweep retries the batch.
func TestRefreshUserFacts_TransientFailureKeepsCursor(t *testing.T) {
	store := &fakeThreadStore{
		FactCursor: &chat.UserFactCursor{LastMessageID: "m0"},
		Messages:   []chat.Message{msg("m1", "thr_a", "hi", time.Now().Add(-time.Hour))},
	}
	transient := errors.New("connection reset")
	s := &server{thread: store, llm: fakeChatClient{FactErr: transient}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, 0); !errors.Is(err, transient) || isFactRefreshSkippable(err) {
		t.Fatalf("err = %v, want the transient error, not skippable", err)
	}
	if len(store.FactBatches) != 0 || store.FactCursor.LastMessageID != "m0" {
		t.Fatalf("a failed extraction applied or moved the cursor")
	}
}

// A single message the model can never fold in (cut off or malformed every
// time) is skipped, so it cannot freeze the user's memory for good.
func TestRefreshUserFacts_UnfoldableMessageIsSkipped(t *testing.T) {
	for _, failure := range []error{llm.ErrMemoryTruncated, llm.ErrFactReplyMalformed} {
		store := &fakeThreadStore{
			FactCursor:     &chat.UserFactCursor{LastMessageID: "m0"},
			ForgottenFacts: []string{"Works at Acme"},
			Messages:       []chat.Message{msg("m1", "thr_a", "hi", time.Now().Add(-time.Hour))},
		}
		s := &server{thread: store, llm: fakeChatClient{FactErr: failure}}
		if err := s.refreshUserFactsIfDue(context.Background(), testUser, 0); err != nil {
			t.Fatalf("%v: err = %v, want the message skipped", failure, err)
		}
		if store.FactCursor.LastMessageID != "m1" || len(store.FactBatches[0].Ops) != 0 {
			t.Fatalf("%v: cursor %q, ops %d; want past the message with nothing applied", failure, store.FactCursor.LastMessageID, len(store.FactBatches[0].Ops))
		}
		if store.FactBatches[0].PurgeForgottenBefore != nil {
			t.Fatalf("%v: a skipped batch purged forgotten facts the model never saw", failure)
		}
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
	if store.FactBatches[0].PurgeForgottenBefore == nil || len(store.ForgottenFacts) != 0 {
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
	if conv.Origin != chat.FactOriginConverted || len(conv.Ops) != 1 || conv.Ops[0].Kind != chat.FactOpAdd {
		t.Fatalf("conversion batch = %+v, want converted adds only", conv)
	}
	if req := <-requests; strings.Contains(req.Transcript, "already in the memory text") || !strings.Contains(req.Transcript, "moved to Bern") {
		t.Fatalf("after conversion the refresh must read only messages newer than the memory text:\n%s", req.Transcript)
	}
}

// The old memory's time layers survive conversion: each section is converted
// on its own and dated into its bucket, identity sections become long-term.
func TestConvertUserMemory_KeepsSectionAges(t *testing.T) {
	blobAt := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	store := &fakeThreadStore{UserMemory: chat.UserMemory{UpdatedAt: &blobAt, Content: "## Work context\n- Backend dev\n\n## Top of mind\n- Shipping v2\n\n## Brief history\n### Recent months\n- Started climbing\n\n### Earlier context\n- Old project"}}
	s := &server{thread: store, llm: fakeChatClient{ConvertedOps: []llm.FactOp{{Op: "add", Category: "work", Content: "x"}}}}

	if _, err := s.convertUserMemory(context.Background(), testUser); err != nil {
		t.Fatalf("convert: %v", err)
	}
	ops := store.FactBatches[0].Ops
	if len(ops) != 4 {
		t.Fatalf("ops = %d, want one per non-empty section (the Brief history umbrella has none)", len(ops))
	}
	day := 24 * time.Hour
	want := []struct {
		seen    time.Time
		durable bool
	}{{blobAt, true}, {blobAt, false}, {blobAt.Add(-60 * day), false}, {blobAt.Add(-200 * day), false}}
	for i, w := range want {
		if !ops[i].SeenAt.Equal(w.seen) || ops[i].Durable != w.durable {
			t.Fatalf("op %d = seen %v durable %v, want %v %v", i, ops[i].SeenAt, ops[i].Durable, w.seen, w.durable)
		}
	}
	if (chat.UserFact{LastSeen: ops[3].SeenAt}).Bucket(blobAt) != chat.BucketEarlier {
		t.Fatalf("Earlier context did not land in the Earlier bucket")
	}
}

// Without old memory text the first refresh reads only the recent past, not
// the whole history.
func TestRefreshUserFacts_NewUserBackfillsRecentOnly(t *testing.T) {
	store := &fakeThreadStore{Messages: []chat.Message{
		msg("ancient", "thr_a", "two years ago", time.Now().Add(-700*24*time.Hour)),
		msg("recent", "thr_a", "last week", time.Now().Add(-7*24*time.Hour)),
	}}
	requests := make(chan fakeFactRequest, 1)
	s := &server{thread: store, llm: fakeChatClient{FactRequests: requests}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, memoryUserRefreshAge); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if req := <-requests; strings.Contains(req.Transcript, "two years ago") || !strings.Contains(req.Transcript, "last week") {
		t.Fatalf("backfill transcript = %q, want only the last %v", req.Transcript, userFactsBackfill)
	}
}

// A batch cursor must not reset the merge pass's date: the pass stays weekly.
func TestRefreshUserFacts_BatchKeepsConsolidationWeekly(t *testing.T) {
	recent := time.Now().Add(-2 * 24 * time.Hour)
	var facts []chat.UserFact
	for i := range userFactsConsolidateMin {
		facts = append(facts, fact(string(rune('a'+i)), "work", "fact", false, time.Now()))
	}
	store := &fakeThreadStore{
		FactCursor: &chat.UserFactCursor{LastConsolidatedAt: &recent},
		Facts:      facts,
		Messages:   []chat.Message{msg("m1", "thr_a", "hi", time.Now().Add(-time.Hour))},
	}
	s := &server{thread: store, llm: fakeChatClient{FactMerges: []llm.FactMerge{{IDs: []string{"a", "b"}, Content: "merged"}}}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, 0); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(store.FactBatches) != 1 || len(store.Merges) != 0 {
		t.Fatalf("batches %d, merges %d: want the batch applied and no merge two days after the last", len(store.FactBatches), len(store.Merges))
	}
}

// truncatingFacts cuts the reply off for any batch of more than maxMessages.
type truncatingFacts struct {
	fakeChatClient
	maxMessages int
	sizes       *[]int
}

func (f truncatingFacts) ExtractUserFacts(_ context.Context, _ []llm.FactInput, transcript, _ string, _ []string, _ string) ([]llm.FactOp, error) {
	n := strings.Count(transcript, "User: ")
	*f.sizes = append(*f.sizes, n)
	if n > f.maxMessages {
		return nil, llm.ErrMemoryTruncated
	}
	return nil, nil
}

// A batch whose reply is cut off is halved until it fits, so a dense backlog
// still moves instead of retrying the same batch every sweep.
func TestRefreshUserFacts_HalvesATruncatedBatch(t *testing.T) {
	var messages []chat.Message
	for i := range 8 {
		messages = append(messages, msg(fmt.Sprintf("m%d", i), "thr_a", "hi", time.Now().Add(-time.Hour+time.Duration(i)*time.Second)))
	}
	store := &fakeThreadStore{FactCursor: &chat.UserFactCursor{}, Messages: messages}
	var sizes []int
	s := &server{thread: store, llm: truncatingFacts{maxMessages: 2, sizes: &sizes}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, 0); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if store.FactCursor.LastMessageID != "m7" {
		t.Fatalf("cursor at %q, want the whole backlog folded in by halved batches (sizes %v)", store.FactCursor.LastMessageID, sizes)
	}
	if sizes[0] != 8 || sizes[1] != 4 || sizes[2] != 2 {
		t.Fatalf("batch sizes = %v, want 8, 4, 2, ...", sizes)
	}
}

// A batch purges only facts forgotten before its own last message: an older
// backlog batch keeps a newer forget on the list, and the 60s settle window
// can never drop the list before the forget turn has been read.
func TestRefreshUserFacts_PurgeCutoffIsTheBatchsLastMessage(t *testing.T) {
	var messages []chat.Message
	base := time.Now().Add(-2 * time.Hour)
	for i := range chat.UserFactBatchLimit + 1 {
		messages = append(messages, msg(fmt.Sprintf("m%05d", i), "thr_a", "x", base.Add(time.Duration(i)*time.Second)))
	}
	store := &fakeThreadStore{FactCursor: &chat.UserFactCursor{}, ForgottenFacts: []string{"Works at Acme"}, Messages: messages}
	s := &server{thread: store, llm: fakeChatClient{}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, 0); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(store.FactBatches) != 2 {
		t.Fatalf("batches = %d, want 2", len(store.FactBatches))
	}
	first, second := store.FactBatches[0].PurgeForgottenBefore, store.FactBatches[1].PurgeForgottenBefore
	if first == nil || !first.Equal(messages[chat.UserFactBatchLimit-1].CreatedAt) {
		t.Fatalf("first batch cutoff = %v, want its last message's date", first)
	}
	// The fake drops the forgotten list after the first purge, so the second
	// batch has nothing left to purge.
	if second != nil {
		t.Fatalf("second batch cutoff = %v, want none once the list is empty", second)
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
	transcript, labels, _, used := factsTranscript(messages)
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

// A new user's backfill start is stored at once: a cursor written later by the
// merge pass must not reset it to the start of the history.
func TestRefreshUserFacts_BackfillCursorIsStored(t *testing.T) {
	store := &fakeThreadStore{Messages: []chat.Message{msg("old", "thr_a", "ancient", time.Now().Add(-400*24*time.Hour))}}
	s := &server{thread: store, llm: fakeChatClient{}}
	if err := s.refreshUserFactsIfDue(context.Background(), testUser, 0); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if store.FactCursor == nil || store.FactCursor.LastCreatedAt == "" {
		t.Fatalf("cursor = %+v, want the backfill start stored", store.FactCursor)
	}
}

func TestSectionAge_GermanHeadingsAndUnknown(t *testing.T) {
	updated := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		heading        string
		durable, known bool
		back           time.Duration
	}{
		{"arbeitskontext", true, true, 0},
		{"persönlicher kontext", true, true, 0},
		{"langfristiger hintergrund", true, true, 0},
		{"letzte monate", false, true, 60 * 24 * time.Hour},
		{"früherer kontext", false, true, 200 * 24 * time.Hour},
		{"something else", false, false, 0},
	} {
		seen, durable, known := sectionAge(tc.heading, updated)
		if durable != tc.durable || known != tc.known || !seen.Equal(updated.Add(-tc.back)) {
			t.Fatalf("%q = %v %v %v, want %v %v -%v", tc.heading, seen, durable, known, tc.durable, tc.known, tc.back)
		}
	}
}

// Each op is dated by the newest message of its own threads, not the batch's.
func TestToFactOps_DatesByTheOpsThreads(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := early.Add(80 * 24 * time.Hour)
	ops := toFactOps([]llm.FactOp{{Op: "add", Threads: []string{"T1"}}, {Op: "add"}},
		map[string]string{"T1": "thr_a", "T2": "thr_b"}, map[string]time.Time{"T1": early, "T2": late})
	if !ops[0].SeenAt.Equal(early) || !ops[1].SeenAt.IsZero() {
		t.Fatalf("seen = %v / %v, want T1's date and the batch default", ops[0].SeenAt, ops[1].SeenAt)
	}
}

// A merge reply that cannot fit or parse skips this week's pass instead of
// retrying it every hourly sweep.
func TestConsolidate_UnparseableReplySkipsTheWeek(t *testing.T) {
	var facts []chat.UserFact
	for i := range userFactsConsolidateMin {
		facts = append(facts, fact(string(rune('a'+i)), "work", "fact", false, time.Now()))
	}
	store := &fakeThreadStore{FactCursor: &chat.UserFactCursor{}, Facts: facts}
	s := &server{thread: store, llm: malformedMerges{}}
	if err := s.consolidateUserFactsIfDue(context.Background(), testUser, chat.UserFactCursor{}); err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	if store.FactCursor.LastConsolidatedAt == nil {
		t.Fatalf("a failed merge pass must still be stamped")
	}
}

type malformedMerges struct{ fakeChatClient }

func (malformedMerges) ConsolidateUserFacts(context.Context, []llm.FactInput, string) ([]llm.FactMerge, error) {
	return nil, llm.ErrFactReplyMalformed
}
