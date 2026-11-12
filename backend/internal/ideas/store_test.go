package ideas

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/store"
)

var today = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type fixture struct {
	db     *sql.DB
	store  *Store
	user   string
	other  string
	thread string
	msg    string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "ideas.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u1', 's1', 'alice', 'user'), ('u2', 's2', 'bob', 'user')`)
	exec(`INSERT INTO threads (id, user_id, title) VALUES ('t1', 'u1', 'Tech-News KW 45'), ('t2', 'u1', 'Other'), ('tb', 'u2', 'Bob')`)
	exec(`INSERT INTO messages (id, thread_id, user_id, role, content) VALUES ('m1', 't1', 'u1', 'assistant', 'Vibe Kanban …'), ('m2', 't2', 'u1', 'assistant', 'x')`)
	return &fixture{db: db, store: NewStore(db), user: "u1", other: "u2", thread: "t1", msg: "m1"}
}

func (f *fixture) create(t *testing.T, in Input) Idea {
	t.Helper()
	idea, err := f.store.Create(context.Background(), f.user, StateOpen, in, today)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return idea
}

func TestCreate_CleansSnapshotsAndRequiresAReason(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	idea := f.create(t, Input{
		Title:     "Vibe\nKanban" + string(lineSeparator) + "board" + string(rune(0x202E)),
		Reason:    "For the\r\nlab week\u0085 deadline",
		Excerpt:   "line one\nline two\x07",
		ThreadID:  f.thread,
		MessageID: f.msg,
	})
	if idea.Title != "Vibe Kanban board" || idea.Reason != "For the lab week deadline" || idea.Excerpt != "line one\nline two" {
		t.Fatalf("cleaned = %q / %q / %q", idea.Title, idea.Reason, idea.Excerpt)
	}
	if idea.State != StateOpen || idea.ThreadTitle != "Tech-News KW 45" || idea.ThreadDeleted || *idea.ThreadID != f.thread || *idea.MessageID != f.msg {
		t.Fatalf("idea = %+v", idea)
	}
	if _, err := f.store.Create(ctx, f.user, StateOpen, Input{Title: "x", Reason: " \n\t "}, today); !isValidation(err) {
		t.Fatalf("blank reason: err = %v, want a validation error", err)
	}
	long := f.create(t, Input{Reason: strings.Repeat("é", 900), Excerpt: strings.Repeat("x", 2000)})
	if n := len([]rune(long.Reason)); n != MaxReasonRunes || !strings.HasSuffix(long.Reason, "…") {
		t.Fatalf("reason %d runes", n)
	}
	if n := len([]rune(long.Title)); n != MaxTitleRunes {
		t.Fatalf("title fallback from the reason: %d runes", n)
	}
	if n := len([]rune(long.Excerpt)); n != MaxExcerptRunes {
		t.Fatalf("excerpt %d runes", n)
	}
	noThread := f.create(t, Input{Title: "Rye", Reason: "too mild"})
	if noThread.ThreadID != nil || noThread.ThreadDeleted || noThread.ThreadTitle != "" {
		t.Fatalf("idea without a thread = %+v", noThread)
	}
}

func TestCreate_ThreadAndMessageMustBeTheUsers(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	for name, in := range map[string]Input{
		"foreign thread":            {Reason: "r", ThreadID: "tb"},
		"unknown thread":            {Reason: "r", ThreadID: "nope"},
		"message of another thread": {Reason: "r", ThreadID: f.thread, MessageID: "m2"},
	} {
		if _, err := f.store.Create(ctx, f.user, StateOpen, in, today); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
	if _, err := f.store.Create(ctx, f.user, StateOpen, Input{Reason: "r", MessageID: f.msg}, today); !isValidation(err) {
		t.Errorf("message without thread: err = %v", err)
	}
}

func TestDefaultRemindOn(t *testing.T) {
	day := func(s string) time.Time { d, _ := ParseDate(s); return d }
	for _, c := range []struct{ due, want string }{
		{"2026-01-12", "2026-01-05"}, // a week before
		{"2026-01-05", "2026-01-04"}, // a week before is past: the day before
		{"2026-01-02", "2026-01-01"}, // the day before is today, not after: today
		{"2026-01-01", "2026-01-01"},
		{"2025-12-20", ""}, // a passed due date: no follow-up
	} {
		got := ""
		if r, ok := DefaultRemindOn(day(c.due), today); ok {
			got = FormatDate(r)
		}
		if got != c.want {
			t.Errorf("due %s: remind %q, want %q", c.due, got, c.want)
		}
	}
}

// Review fixes: a passed due date saves without a follow-up; sending the
// stored due date back keeps a hand-picked follow-up; a patch that moves the
// follow-up and hides it in one go stays hidden; a stale proposal cannot be
// confirmed; an Arabic letter mark is a bidi control too.
func TestDates_EdgeCases(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	past := f.create(t, Input{Reason: "r", DueOn: DateOf("2025-12-20")})
	if past.RemindOn != nil || *past.DueOn != "2025-12-20" {
		t.Fatalf("past due = %+v", past)
	}
	picked := f.create(t, Input{Reason: "r", DueOn: DateOf("2026-02-20"), RemindOn: DateOf("2026-02-01")})
	title := "new"
	same, err := f.store.Update(ctx, f.user, picked.ID, Patch{Title: &title, DueOn: DateOf("2026-02-20")}, today)
	if err != nil || *same.RemindOn != "2026-02-01" {
		t.Fatalf("resent due date changed the follow-up: %+v %v", same.RemindOn, err)
	}
	hide := true
	both, err := f.store.Update(ctx, f.user, picked.ID, Patch{RemindOn: DateOf("2026-02-10"), RemindDismissed: &hide}, today)
	if err != nil || both.RemindDismissedAt == nil || *both.RemindOn != "2026-02-10" {
		t.Fatalf("move + hide = %+v %v", both, err)
	}
	stale, _ := f.store.Create(ctx, f.user, StateProposed, Input{Reason: "r"}, today)
	if _, err := f.db.Exec(`UPDATE ideas SET created_at = datetime('now', '-31 days') WHERE id = ?`, stale.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Confirm(ctx, f.user, stale.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale proposal confirmed: %v", err)
	}
	if got := CleanLine("a"+string(rune(0x061C))+"b", 10); got != "ab" {
		t.Fatalf("U+061C kept: %q", got)
	}
}

func TestDates_ThreeStates(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	withDefault := f.create(t, Input{Reason: "r", DueOn: DateOf("2026-01-12")})
	if *withDefault.DueOn != "2026-01-12" || *withDefault.RemindOn != "2026-01-05" {
		t.Fatalf("default follow-up: %v %v", *withDefault.DueOn, *withDefault.RemindOn)
	}
	none := f.create(t, Input{Reason: "r", DueOn: DateOf("2026-01-12"), RemindOn: OptionalDate{Set: true}})
	if none.RemindOn != nil {
		t.Fatalf("remindOn null must mean no follow-up: %v", *none.RemindOn)
	}
	explicit := f.create(t, Input{Reason: "r", RemindOn: DateOf("2026-02-01")})
	if explicit.DueOn != nil || *explicit.RemindOn != "2026-02-01" {
		t.Fatalf("follow-up without a due date: %+v", explicit)
	}
	for _, in := range []Input{
		{Reason: "r", DueOn: DateOf("12.1.2026")},
		{Reason: "r", DueOn: DateOf("2026-01-12"), RemindOn: DateOf("2026-01-13")},
	} {
		if _, err := f.store.Create(ctx, f.user, StateOpen, in, today); !isValidation(err) {
			t.Errorf("%+v: err = %v", in, err)
		}
	}
	// PATCH: a due date alone recomputes the default; a changed follow-up
	// starts over (not yet notified, not dismissed).
	if _, err := f.db.Exec(`UPDATE ideas SET reminded_at = datetime('now'), remind_dismissed_at = datetime('now') WHERE id = ?`, withDefault.ID); err != nil {
		t.Fatal(err)
	}
	moved, err := f.store.Update(ctx, f.user, withDefault.ID, Patch{DueOn: DateOf("2026-02-12")}, today)
	if err != nil {
		t.Fatal(err)
	}
	if *moved.RemindOn != "2026-02-05" || moved.RemindedAt != nil || moved.RemindDismissedAt != nil {
		t.Fatalf("moved = remind %v reminded %v dismissed %v", *moved.RemindOn, moved.RemindedAt, moved.RemindDismissedAt)
	}
	cleared, err := f.store.Update(ctx, f.user, withDefault.ID, Patch{RemindOn: OptionalDate{Set: true}}, today)
	if err != nil || cleared.RemindOn != nil || *cleared.DueOn != "2026-02-12" {
		t.Fatalf("× on the follow-up chip: %+v %v", cleared, err)
	}
}

// A proposal is invisible everywhere but Confirm, GetAny and Delete (its
// card's Dismiss, which discards it for good).
func TestProposalsStayInvisibleUntilConfirmed(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	p, err := f.store.Create(ctx, f.user, StateProposed, Input{Title: "t", Reason: "r", ThreadID: f.thread}, today)
	if err != nil {
		t.Fatal(err)
	}
	if items, _ := f.store.List(ctx, f.user, StateOpen, today); len(items) != 0 {
		t.Fatalf("proposal listed: %+v", items)
	}
	if open, done, _ := f.store.Count(ctx, f.user); open != 0 || done != 0 {
		t.Fatalf("proposal counted: %d %d", open, done)
	}
	if _, err := f.store.Get(ctx, f.user, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get proposal: %v", err)
	}
	title := "x"
	if _, err := f.store.Update(ctx, f.user, p.ID, Patch{Title: &title}, today); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update proposal: %v", err)
	}
	done := true
	if _, err := f.store.Update(ctx, f.user, p.ID, Patch{Done: &done}, today); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mark proposal done: %v", err)
	}
	if _, err := f.store.Restore(ctx, f.user, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Restore proposal: %v", err)
	}
	if _, err := f.store.Confirm(ctx, f.other, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another user confirmed it: %v", err)
	}
	confirmed, err := f.store.Confirm(ctx, f.user, p.ID)
	if err != nil || confirmed.State != StateOpen {
		t.Fatalf("Confirm = %+v, %v", confirmed, err)
	}
	if _, err := f.store.Confirm(ctx, f.user, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Confirm: %v", err)
	}
	dismissed, _ := f.store.Create(ctx, f.user, StateProposed, Input{Reason: "r"}, today)
	if err := f.store.Delete(ctx, f.user, dismissed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetAny(ctx, f.user, dismissed.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dismissed proposal still stored: %v", err)
	}
}

func TestDeleteRestoreAndPurge(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	idea := f.create(t, Input{Title: "a", Reason: "r"})
	if err := f.store.Delete(ctx, f.other, idea.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign delete: %v", err)
	}
	if err := f.store.Delete(ctx, f.user, idea.ID); err != nil {
		t.Fatal(err)
	}
	if items, _ := f.store.List(ctx, f.user, StateOpen, today); len(items) != 0 {
		t.Fatalf("deleted idea listed")
	}
	if err := f.store.Delete(ctx, f.user, idea.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	restored, err := f.store.Restore(ctx, f.user, idea.ID)
	if err != nil || restored.DeletedAt != nil {
		t.Fatalf("Restore = %+v, %v", restored, err)
	}

	old := f.create(t, Input{Title: "old", Reason: "r"})
	stale, _ := f.store.Create(ctx, f.user, StateProposed, Input{Reason: "r"}, today)
	fresh, _ := f.store.Create(ctx, f.user, StateProposed, Input{Reason: "r"}, today)
	recent := f.create(t, Input{Title: "recent", Reason: "r"})
	_ = f.store.Delete(ctx, f.user, old.ID)
	_ = f.store.Delete(ctx, f.user, recent.ID)
	if _, err := f.db.Exec(`UPDATE ideas SET deleted_at = datetime('now', '-31 days') WHERE id = ?`, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE ideas SET created_at = datetime('now', '-31 days') WHERE id = ?`, stale.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.Restore(ctx, f.user, old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restored after 30 days: %v", err)
	}
	if _, err := f.store.List(ctx, f.user, StateOpen, today); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{old.ID: false, stale.ID: false, fresh.ID: true, recent.ID: true} {
		_, err := f.store.GetAny(ctx, f.user, id)
		if got := err == nil; got != want {
			t.Errorf("%s kept = %v, want %v", id, got, want)
		}
	}
}

func TestListOrderCountAndDone(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	first := f.create(t, Input{Title: "first", Reason: "r"})
	due := f.create(t, Input{Title: "due", Reason: "r", RemindOn: DateOf("2026-01-01")})
	later := f.create(t, Input{Title: "later", Reason: "r", RemindOn: DateOf("2026-03-01")})
	if _, err := f.db.Exec(`UPDATE ideas SET created_at = datetime('now', '-1 day') WHERE id = ?`, due.ID); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.List(ctx, f.user, StateOpen, today)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(items); got != "due,later,first" {
		t.Fatalf("open order = %s, want the due follow-up first, then newest", got)
	}
	done := true
	if _, err := f.store.Update(ctx, f.user, first.ID, Patch{Done: &done}, today); err != nil {
		t.Fatal(err)
	}
	if open, d, _ := f.store.Count(ctx, f.user); open != 2 || d != 1 {
		t.Fatalf("count = %d open %d done", open, d)
	}
	doneItems, _ := f.store.List(ctx, f.user, StateDone, today)
	if len(doneItems) != 1 || doneItems[0].DoneAt == nil {
		t.Fatalf("done list = %+v", doneItems)
	}
	reopen := false
	back, _ := f.store.Update(ctx, f.user, first.ID, Patch{Done: &reopen}, today)
	if back.State != StateOpen || back.DoneAt != nil {
		t.Fatalf("reopened = %+v", back)
	}
	if items, _ := f.store.List(ctx, f.other, StateOpen, today); len(items) != 0 {
		t.Fatalf("another user sees %d ideas", len(items))
	}
	if _, err := f.store.Get(ctx, f.other, later.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign Get: %v", err)
	}
	if _, err := f.store.List(ctx, f.user, "proposed", today); !isValidation(err) {
		t.Fatalf("list proposals: %v", err)
	}
}

// An idea survives its deleted thread unchanged: the thread's title stays,
// marked deleted, and the excerpt, title and reason are untouched.
func TestIdeaSurvivesItsThread(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	idea := f.create(t, Input{Title: "t", Reason: "r", Excerpt: "e", ThreadID: f.thread, MessageID: f.msg})
	if _, err := f.db.Exec(`DELETE FROM threads WHERE id = ?`, f.thread); err != nil {
		t.Fatal(err)
	}
	got, err := f.store.Get(ctx, f.user, idea.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ThreadID != nil || got.MessageID != nil || !got.ThreadDeleted || got.ThreadTitle != "Tech-News KW 45" ||
		got.Title != "t" || got.Reason != "r" || got.Excerpt != "e" {
		t.Fatalf("after thread delete = %+v", got)
	}
}

func titles(items []Idea) string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return strings.Join(out, ",")
}

func isValidation(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}
