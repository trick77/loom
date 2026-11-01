package chat

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func newFactStore(t *testing.T) (*Store, *sql.DB, string) {
	t.Helper()
	db := openTestDB(t)
	return NewStore(db), db, insertTestUser(t, db, "alice")
}

func newThread(t *testing.T, store *Store, userID string) string {
	t.Helper()
	thread, err := store.CreateThread(context.Background(), userID, CreateThreadInput{Title: "t"})
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	return thread.ID
}

func applyBatch(t *testing.T, store *Store, userID string, batch UserFactBatch) UserFactApplyResult {
	t.Helper()
	if batch.Origin == "" {
		batch.Origin = FactOriginExtracted
	}
	if batch.SeenAt.IsZero() {
		batch.SeenAt = time.Now()
	}
	res, err := store.ApplyUserFactBatch(context.Background(), userID, batch)
	if err != nil {
		t.Fatalf("ApplyUserFactBatch: %v", err)
	}
	return res
}

func activeFacts(t *testing.T, store *Store, userID string) []UserFact {
	t.Helper()
	facts, err := store.ListActiveUserFacts(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListActiveUserFacts: %v", err)
	}
	return facts
}

func factByContent(facts []UserFact, content string) (UserFact, bool) {
	for _, f := range facts {
		if f.Content == content {
			return f, true
		}
	}
	return UserFact{}, false
}

func TestApplyUserFactBatch_AddUpdateReinforce(t *testing.T) {
	store, _, userID := newFactStore(t)
	thread := newThread(t, store, userID)
	day1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

	res := applyBatch(t, store, userID, UserFactBatch{SeenAt: day1, Threads: []string{thread}, Ops: []UserFactOp{
		{Kind: FactOpAdd, Category: FactCategoryWork, Content: "Backend dev at Acme"},
		{Kind: FactOpAdd, Category: FactCategoryPersonal, Content: "Lives Zurich", Durable: true},
	}})
	if res.Added != 2 {
		t.Fatalf("added = %d, want 2", res.Added)
	}
	facts := activeFacts(t, store, userID)
	work, _ := factByContent(facts, "Backend dev at Acme")
	if !work.FirstSeen.Equal(day1) || !work.LastSeen.Equal(day1) {
		t.Fatalf("dates = %v/%v, want the batch's message date", work.FirstSeen, work.LastSeen)
	}

	day9 := day1.Add(8 * 24 * time.Hour)
	res = applyBatch(t, store, userID, UserFactBatch{SeenAt: day9, Threads: []string{thread}, Ops: []UserFactOp{
		{Kind: FactOpUpdate, ID: work.ID, Content: "Staff backend dev at Acme"},
	}})
	if res.Updated != 1 {
		t.Fatalf("updated = %d, want 1", res.Updated)
	}
	got, ok := factByContent(activeFacts(t, store, userID), "Staff backend dev at Acme")
	if !ok || !got.LastSeen.Equal(day9) || !got.FirstSeen.Equal(day1) {
		t.Fatalf("updated fact = %+v, want new content, last_seen bumped, first_seen kept", got)
	}

	// An older batch never moves last_seen backwards.
	applyBatch(t, store, userID, UserFactBatch{SeenAt: day1, Threads: []string{thread}, Ops: []UserFactOp{{Kind: FactOpReinforce, ID: work.ID}}})
	got, _ = factByContent(activeFacts(t, store, userID), "Staff backend dev at Acme")
	if !got.LastSeen.Equal(day9) {
		t.Fatalf("last_seen = %v, want it kept at the later %v", got.LastSeen, day9)
	}
}

func TestApplyUserFactBatch_DropsInvalidOps(t *testing.T) {
	store, db, userID := newFactStore(t)
	other := insertTestUser(t, db, "bob")
	thread := newThread(t, store, userID)
	bobThread := newThread(t, store, other)
	applyBatch(t, store, other, UserFactBatch{Threads: []string{bobThread}, Ops: []UserFactOp{{Kind: FactOpAdd, Category: FactCategoryWork, Content: "bob fact"}}})
	bobFact := activeFacts(t, store, other)[0]

	res := applyBatch(t, store, userID, UserFactBatch{Threads: []string{thread}, Ops: []UserFactOp{
		{Kind: FactOpAdd, Category: "history", Content: "time is not a category"},
		{Kind: FactOpAdd, Category: FactCategoryWork, Content: strings.Repeat("x", MaxUserFactLength+1)},
		{Kind: FactOpAdd, Category: FactCategoryWork, Content: "   "},
		{Kind: FactOpUpdate, ID: "nope", Content: "x"},
		{Kind: FactOpReinforce, ID: bobFact.ID},
		{Kind: FactOpRetire, ID: bobFact.ID, SupersededBy: bobFact.ID},
		{Kind: "delete", ID: "x"},
	}})
	if res.Added != 0 || res.Dropped != 7 {
		t.Fatalf("result = %+v, want all 7 ops dropped", res)
	}
	if len(activeFacts(t, store, other)) != 1 {
		t.Fatalf("another user's fact was touched")
	}
}

func TestApplyUserFactBatch_RetireNeedsANamedReplacement(t *testing.T) {
	store, _, userID := newFactStore(t)
	thread := newThread(t, store, userID)
	applyBatch(t, store, userID, UserFactBatch{Threads: []string{thread}, Ops: []UserFactOp{
		{Kind: FactOpAdd, Category: FactCategoryWork, Content: "Works at Acme", Durable: true},
		{Kind: FactOpAdd, Category: FactCategoryPersonal, Content: "Lives Bern"},
	}})
	acme, _ := factByContent(activeFacts(t, store, userID), "Works at Acme")
	bern, _ := factByContent(activeFacts(t, store, userID), "Lives Bern")

	res := applyBatch(t, store, userID, UserFactBatch{Threads: []string{thread}, Ops: []UserFactOp{
		{Kind: FactOpRetire, ID: acme.ID},                        // no replacement: "it's old" is not a reason
		{Kind: FactOpRetire, ID: bern.ID, SupersededBy: "ghost"}, // unknown replacement
		{Kind: FactOpRetire, ID: acme.ID, SupersededBy: "new-job"},
		{Kind: FactOpAdd, Ref: "new-job", Category: FactCategoryWork, Content: "Works at Initech"},
	}})
	if res.Retired != 1 || res.Dropped != 2 || res.Added != 1 {
		t.Fatalf("result = %+v, want 1 retired (by the batch's add), 2 dropped", res)
	}
	facts := activeFacts(t, store, userID)
	if _, ok := factByContent(facts, "Works at Acme"); ok {
		t.Fatalf("superseded durable fact still active")
	}
	if _, ok := factByContent(facts, "Lives Bern"); !ok {
		t.Fatalf("fact retired without a valid replacement")
	}
}

func TestApplyUserFactBatch_PromotesAfterManyMonths(t *testing.T) {
	store, _, userID := newFactStore(t)
	thread := newThread(t, store, userID)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	applyBatch(t, store, userID, UserFactBatch{SeenAt: start, Threads: []string{thread}, Ops: []UserFactOp{
		{Kind: FactOpAdd, Category: FactCategoryInterest, Content: "Climbs"},
	}})
	climbs := activeFacts(t, store, userID)[0]

	applyBatch(t, store, userID, UserFactBatch{SeenAt: start.Add(179 * 24 * time.Hour), Threads: []string{thread}, Ops: []UserFactOp{{Kind: FactOpReinforce, ID: climbs.ID}}})
	if activeFacts(t, store, userID)[0].Durable {
		t.Fatalf("promoted before %v", UserFactDurableSpan)
	}
	applyBatch(t, store, userID, UserFactBatch{SeenAt: start.Add(UserFactDurableSpan), Threads: []string{thread}, Ops: []UserFactOp{{Kind: FactOpReinforce, ID: climbs.ID}}})
	if !activeFacts(t, store, userID)[0].Durable {
		t.Fatalf("not promoted after %v of reinforcement", UserFactDurableSpan)
	}
}

func TestUserFacts_ThreadDeleteRetiresOnlyOrphanedExtractedFacts(t *testing.T) {
	ctx := context.Background()
	store, _, userID := newFactStore(t)
	gone := newThread(t, store, userID)
	kept := newThread(t, store, userID)
	applyBatch(t, store, userID, UserFactBatch{Threads: []string{gone}, Ops: []UserFactOp{
		{Kind: FactOpAdd, Category: FactCategoryWork, Content: "only in gone"},
		{Kind: FactOpAdd, Category: FactCategoryWork, Content: "durable in gone", Durable: true},
		{Kind: FactOpAdd, Category: FactCategoryWork, Content: "in both", Threads: []string{gone, kept}},
	}})
	applyBatch(t, store, userID, UserFactBatch{Origin: FactOriginConverted, Ops: []UserFactOp{
		{Kind: FactOpAdd, Category: FactCategoryPersonal, Content: "converted"},
	}})
	if _, err := store.AddUserFact(ctx, userID, gone, FactCategoryPersonal, "said remember in gone", false); err != nil {
		t.Fatalf("AddUserFact: %v", err)
	}

	if _, err := store.DeleteThread(ctx, userID, gone); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	facts := activeFacts(t, store, userID)
	if _, ok := factByContent(facts, "only in gone"); ok {
		t.Fatalf("orphaned extracted fact still shown after its thread was deleted")
	}
	for _, want := range []string{"durable in gone", "in both", "converted", "said remember in gone"} {
		if _, ok := factByContent(facts, want); !ok {
			t.Fatalf("fact %q lost by the thread delete", want)
		}
	}
	// The next refresh retires it for good.
	applyBatch(t, store, userID, UserFactBatch{})
	var reason string
	if err := store.db.QueryRowContext(ctx, `SELECT retire_reason FROM user_facts WHERE content = 'only in gone'`).Scan(&reason); err != nil || reason != FactRetiredThreadDeleted {
		t.Fatalf("retire_reason = %q (%v), want thread_deleted", reason, err)
	}
}

func TestUserFacts_ForgetThenPurgeOnNextRefresh(t *testing.T) {
	ctx := context.Background()
	store, _, userID := newFactStore(t)
	thread := newThread(t, store, userID)
	applyBatch(t, store, userID, UserFactBatch{Threads: []string{thread}, Ops: []UserFactOp{
		{Kind: FactOpAdd, Category: FactCategoryWork, Content: "Works at Acme", Durable: true},
		{Kind: FactOpAdd, Category: FactCategoryWork, Content: "Acme team lead"},
		{Kind: FactOpAdd, Category: FactCategoryPersonal, Content: "Lives Bern"},
	}})
	facts := activeFacts(t, store, userID)
	acme, _ := factByContent(facts, "Works at Acme")
	lead, _ := factByContent(facts, "Acme team lead")

	forgotten, err := store.ForgetUserFacts(ctx, userID, []string{acme.ID, lead.ID, "unknown", acme.ID})
	if err != nil || len(forgotten) != 2 {
		t.Fatalf("ForgetUserFacts = %d facts, %v; want both, durable included", len(forgotten), err)
	}
	if len(activeFacts(t, store, userID)) != 1 {
		t.Fatalf("forgotten facts still active")
	}
	contents, err := store.ListForgottenUserFacts(ctx, userID)
	if err != nil || len(contents) != 2 {
		t.Fatalf("ListForgottenUserFacts = %v, %v; want the two forgotten contents", contents, err)
	}

	applyBatch(t, store, userID, UserFactBatch{PurgeForgotten: true})
	var rows int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_facts WHERE user_id = ? AND content LIKE '%Acme%'`, userID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("forgotten rows left = %d (%v), want hard-deleted after the refresh", rows, err)
	}
}

func TestUserFacts_ToolWritesValidateAndUpdateKeepsDurable(t *testing.T) {
	ctx := context.Background()
	store, _, userID := newFactStore(t)
	if _, err := store.AddUserFact(ctx, userID, "", "history", "x", false); err != ErrUserFactInvalid {
		t.Fatalf("bad category err = %v, want ErrUserFactInvalid", err)
	}
	if _, err := store.AddUserFact(ctx, userID, "", FactCategoryWork, strings.Repeat("x", MaxUserFactLength+1), false); err != ErrUserFactInvalid {
		t.Fatalf("too long err = %v, want ErrUserFactInvalid", err)
	}
	fact, err := store.AddUserFact(ctx, userID, "", FactCategoryWork, "Works at Acme", true)
	if err != nil {
		t.Fatalf("AddUserFact: %v", err)
	}
	updated, found, err := store.UpdateUserFact(ctx, userID, "", fact.ID, "Works at Initech")
	if err != nil || !found || !updated.Durable || updated.Content != "Works at Initech" {
		t.Fatalf("UpdateUserFact = %+v, %v, %v; want new content, durable kept", updated, found, err)
	}
	if _, found, _ := store.UpdateUserFact(ctx, userID, "", "missing", "x"); found {
		t.Fatalf("update of unknown id reported found")
	}
}

func TestMergeUserFacts(t *testing.T) {
	ctx := context.Background()
	store, _, userID := newFactStore(t)
	a := newThread(t, store, userID)
	b := newThread(t, store, userID)
	early := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	applyBatch(t, store, userID, UserFactBatch{SeenAt: late, Threads: []string{a}, Ops: []UserFactOp{{Kind: FactOpAdd, Category: FactCategoryInterest, Content: "Climbs"}}})
	applyBatch(t, store, userID, UserFactBatch{SeenAt: early, Threads: []string{b}, Ops: []UserFactOp{{Kind: FactOpAdd, Category: FactCategoryInterest, Content: "Bouldering", Durable: true}}})
	facts := activeFacts(t, store, userID)
	climbs, _ := factByContent(facts, "Climbs")
	boulder, _ := factByContent(facts, "Bouldering")

	ok, err := store.MergeUserFacts(ctx, userID, []string{climbs.ID, boulder.ID}, "Climbs, mostly bouldering")
	if err != nil || !ok {
		t.Fatalf("MergeUserFacts = %v, %v", ok, err)
	}
	facts = activeFacts(t, store, userID)
	if len(facts) != 1 {
		t.Fatalf("active facts = %d, want 1 after merge", len(facts))
	}
	m := facts[0]
	if m.ID != climbs.ID || !m.Durable || !m.FirstSeen.Equal(early) || !m.LastSeen.Equal(late) {
		t.Fatalf("merged = %+v, want first id kept, durable any, min first_seen, max last_seen", m)
	}
	// Sources were unioned: deleting thread a leaves b holding the fact.
	if _, err := store.DeleteThread(ctx, userID, a); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	if len(activeFacts(t, store, userID)) != 1 {
		t.Fatalf("merged fact lost its unioned source")
	}
	if ok, _ := store.MergeUserFacts(ctx, userID, []string{m.ID, "ghost"}, "x"); ok {
		t.Fatalf("merge with an unknown id must not apply")
	}
}

func TestListUserMessagesAfter_CursorAndSettleMargin(t *testing.T) {
	ctx := context.Background()
	store, db, userID := newFactStore(t)
	thread := newThread(t, store, userID)
	insert := func(id, createdAt string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO messages (id, thread_id, user_id, role, content, created_at) VALUES (?, ?, ?, 'user', ?, ?)`,
			id, thread, userID, "m-"+id, createdAt); err != nil {
			t.Fatalf("insert message: %v", err)
		}
	}
	insert("b", "2026-01-01 10:00:00")
	insert("a", "2026-01-01 10:00:00") // same second, smaller id
	insert("c", "2026-01-01 10:00:05")
	insert("d", "2026-01-01 10:01:00")

	settled := time.Date(2026, 1, 1, 10, 0, 30, 0, time.UTC)
	got, err := store.ListUserMessagesAfter(ctx, userID, "", "", settled, 10)
	if err != nil {
		t.Fatalf("ListUserMessagesAfter: %v", err)
	}
	if ids := messageIDs(got); ids != "a,b,c" {
		t.Fatalf("ids = %s, want a,b,c (ordered by created_at, id; d not yet settled)", ids)
	}
	got, _ = store.ListUserMessagesAfter(ctx, userID, "2026-01-01 10:00:00", "a", settled, 1)
	if ids := messageIDs(got); ids != "b" {
		t.Fatalf("after (10:00:00, a) limit 1 = %s, want b", ids)
	}
	got, _ = store.ListUserMessagesAfter(ctx, userID, "2026-01-01 10:00:05", "c", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), 10)
	if ids := messageIDs(got); ids != "d" {
		t.Fatalf("after c = %s, want d", ids)
	}
}

func messageIDs(messages []Message) string {
	ids := make([]string, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
	}
	return strings.Join(ids, ",")
}

func TestUserFactCursor(t *testing.T) {
	ctx := context.Background()
	store, _, userID := newFactStore(t)
	if _, ok, err := store.GetUserFactCursor(ctx, userID); err != nil || ok {
		t.Fatalf("cursor before any refresh = %v, %v; want none", ok, err)
	}
	applyBatch(t, store, userID, UserFactBatch{Cursor: &UserFactCursor{LastCreatedAt: "2026-01-01 10:00:00", LastMessageID: "m1"}})
	c, ok, err := store.GetUserFactCursor(ctx, userID)
	if err != nil || !ok || c.LastCreatedAt != "2026-01-01 10:00:00" || c.LastMessageID != "m1" || c.LastRefreshAt == nil {
		t.Fatalf("cursor = %+v, %v, %v", c, ok, err)
	}
	if err := store.MarkUserFactsConsolidated(ctx, userID); err != nil {
		t.Fatalf("MarkUserFactsConsolidated: %v", err)
	}
	c, _, _ = store.GetUserFactCursor(ctx, userID)
	if c.LastConsolidatedAt == nil || c.LastMessageID != "m1" {
		t.Fatalf("cursor after consolidation = %+v, want timestamp set and position kept", c)
	}
}

func TestSortUserFactsAndBuckets(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	facts := []UserFact{
		{ID: "earlier", Category: FactCategoryWork, LastSeen: now.Add(-200 * day)},
		{ID: "recent-work", Category: FactCategoryWork, LastSeen: now.Add(-60 * day)},
		{ID: "top-personal", Category: FactCategoryPersonal, LastSeen: now.Add(-1 * day)},
		{ID: "top-work-old", Category: FactCategoryWork, LastSeen: now.Add(-20 * day)},
		{ID: "top-work-new", Category: FactCategoryWork, LastSeen: now.Add(-2 * day)},
		{ID: "long", Category: FactCategoryInterest, Durable: true, LastSeen: now.Add(-900 * day)},
	}
	SortUserFacts(facts, now)
	got := make([]string, len(facts))
	for i, f := range facts {
		got[i] = f.ID
	}
	want := "long,top-work-new,top-work-old,top-personal,recent-work,earlier"
	if strings.Join(got, ",") != want {
		t.Fatalf("order = %s, want %s", strings.Join(got, ","), want)
	}
}
