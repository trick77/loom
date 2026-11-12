package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trick77/loom/internal/ideas"
	"github.com/trick77/loom/internal/schedule"
)

// A follow-up goes out on its date in the user's zone, from 8 in the
// morning, once, with the idea's stored title and reason, and links to the
// idea.
func TestIdeaFollowUpsNotifyOnceInTheUsersMorning(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	if _, err := f.db.Exec(`UPDATE users SET timezone = 'Pacific/Auckland' WHERE id = ?`, testUser.ID); err != nil {
		t.Fatal(err)
	}
	store := ideas.NewStore(f.db)
	idea, err := store.Create(ctx, testUser.ID, ideas.StateOpen, ideas.Input{Title: "Vibe Kanban", Reason: "Lab week", RemindOn: ideas.DateOf("2026-01-05")}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	f.deps.Ideas = store
	s := newServer(f.deps)
	at := func(utc string) {
		now, _ := time.Parse(time.RFC3339, utc)
		s.now = func() time.Time { return now }
		s.sendIdeaFollowUps(ctx)
	}
	count := func() int {
		notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10)
		return len(notes)
	}
	// 4 Jan 18:00 UTC is 5 Jan 07:00 in Auckland: the day, but before 8.
	at("2026-01-04T18:00:00Z")
	if count() != 0 {
		t.Fatal("notified before 8 in the user's zone")
	}
	// 4 Jan 20:00 UTC is 5 Jan 09:00 in Auckland (still the 4th in UTC).
	at("2026-01-04T20:00:00Z")
	at("2026-01-04T20:10:00Z")
	notes, _ := f.store.ListNotifications(ctx, testUser.ID, 10)
	if len(notes) != 1 {
		t.Fatalf("notifications = %d, want exactly one", len(notes))
	}
	n := notes[0]
	if n.Kind != schedule.KindIdeaDue || n.IdeaID == nil || *n.IdeaID != idea.ID || n.Title != "Vibe Kanban" || n.Body != "Lab week" {
		t.Fatalf("notification = %+v", n)
	}
	if got := pushURL(n); got != "/ideas/"+idea.ID {
		t.Fatalf("push url = %q", got)
	}
	// Marking the idea done settles its notice in the bell.
	handler := newAuthenticatedServer(t, f.deps)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authenticatedRequest(http.MethodPatch, "/api/ideas/"+idea.ID, `{"done":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("done = %d %s", rec.Code, rec.Body)
	}
	if unread, _ := f.store.UnreadCount(ctx, testUser.ID); unread != 0 {
		t.Fatalf("unread after done = %d", unread)
	}
}

// A follow-up for today set late in the evening waits for the next morning;
// deleting the idea settles its notice in the bell.
func TestIdeaFollowUpsWaitOutTheNightAndDeleteSettles(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	if _, err := f.db.Exec(`UPDATE users SET timezone = 'Europe/Zurich' WHERE id = ?`, testUser.ID); err != nil {
		t.Fatal(err)
	}
	store := ideas.NewStore(f.db)
	idea, err := store.Create(ctx, testUser.ID, ideas.StateOpen, ideas.Input{Title: "t", Reason: "r", DueOn: ideas.DateOf("2026-01-06"), RemindOn: ideas.DateOf("2026-01-05")}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	f.deps.Ideas = store
	s := newServer(f.deps)
	at := func(utc time.Time) {
		s.now = func() time.Time { return utc }
		s.sendIdeaFollowUps(ctx)
	}
	// 5 Jan 22:30 in Zurich: its day, but night.
	at(time.Date(2026, 1, 5, 21, 30, 0, 0, time.UTC))
	if unread, _ := f.store.UnreadCount(ctx, testUser.ID); unread != 0 {
		t.Fatal("notified at night")
	}
	// 6 Jan 08:30: the next morning, the due day itself.
	at(time.Date(2026, 1, 6, 7, 30, 0, 0, time.UTC))
	if unread, _ := f.store.UnreadCount(ctx, testUser.ID); unread != 1 {
		t.Fatalf("unread next morning = %d", unread)
	}
	handler := newAuthenticatedServer(t, f.deps)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/ideas/"+idea.ID, ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if unread, _ := f.store.UnreadCount(ctx, testUser.ID); unread != 0 {
		t.Fatalf("unread after delete = %d", unread)
	}
}

// A hidden follow-up is never sent; one gone stale (due date passed, or two
// weeks without one) is claimed without a word; candidates not yet due in
// their zone never hide due ones behind a full page.
func TestIdeaFollowUpsSkipHiddenStaleAndPageOn(t *testing.T) {
	ctx := context.Background()
	f := newScheduleFixture(t)
	if _, err := f.db.Exec(`UPDATE users SET timezone = 'Europe/Zurich' WHERE id = ?`, testUser.ID); err != nil {
		t.Fatal(err)
	}
	store := ideas.NewStore(f.db)
	created := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	mk := func(title string, in ideas.Input) ideas.Idea {
		in.Title, in.Reason = title, "r"
		idea, err := store.Create(ctx, testUser.ID, ideas.StateOpen, in, created)
		if err != nil {
			t.Fatal(err)
		}
		return idea
	}
	hidden := mk("hidden", ideas.Input{RemindOn: ideas.DateOf("2026-01-05")})
	yes := true
	if _, err := store.Update(ctx, testUser.ID, hidden.ID, ideas.Patch{RemindDismissed: &yes}, created); err != nil {
		t.Fatal(err)
	}
	passed := mk("due passed", ideas.Input{DueOn: ideas.DateOf("2026-01-04"), RemindOn: ideas.DateOf("2025-12-28")})
	mk("old", ideas.Input{RemindOn: ideas.DateOf("2025-12-01")})
	// More candidates than one page: the sender pages on.
	for i := range ideaFollowUpBatch + 5 {
		mk(fmt.Sprintf("more %d", i), ideas.Input{RemindOn: ideas.DateOf("2026-01-05")})
	}
	due := mk("due", ideas.Input{RemindOn: ideas.DateOf("2026-01-05")})
	f.deps.Ideas = store
	s := newServer(f.deps)
	// 5 Jan 09:00 in Zurich: everything with remind_on 5 Jan is due now.
	s.now = func() time.Time { return time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC) }
	s.sendIdeaFollowUps(ctx)
	notes, _ := f.store.ListNotifications(ctx, testUser.ID, 500)
	titles := map[string]bool{}
	for _, n := range notes {
		titles[n.Title] = true
	}
	if titles["hidden"] || titles["due passed"] || titles["old"] || !titles["due"] {
		t.Fatalf("notified %v", titles)
	}
	if len(notes) != ideaFollowUpBatch+6 {
		t.Fatalf("notifications = %d, want every due one across pages", len(notes))
	}
	got, _ := store.GetAny(ctx, testUser.ID, passed.ID)
	if got.RemindedAt == nil {
		t.Fatal("a stale follow-up stays a candidate forever")
	}
	if got, _ := store.GetAny(ctx, testUser.ID, due.ID); got.RemindedAt == nil {
		t.Fatal("due one not claimed")
	}
}

// The start page lists the follow-ups whose date has come and that the user
// has not hidden.
func TestTodayListsIdeaFollowUps(t *testing.T) {
	ctx := context.Background()
	f := newIdeaFixture(t)
	// The test user keeps no zone: the server's today is UTC's.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// Due tomorrow: the default follow-up is today.
	due := today.AddDate(0, 0, 1).Format(time.DateOnly)
	shown, _ := f.ideas.Create(ctx, testUser.ID, ideas.StateOpen, ideas.Input{Title: "Kanban", Reason: "lab week", DueOn: ideas.DateOf(due)}, today)
	hidden, _ := f.ideas.Create(ctx, testUser.ID, ideas.StateOpen, ideas.Input{Title: "Hidden", Reason: "r", DueOn: ideas.DateOf(due)}, today)
	if rec := f.do(t, "PATCH", "/api/ideas/"+hidden.ID, `{"remindDismissed":true}`); rec.Code != 200 {
		t.Fatalf("hide = %d", rec.Code)
	}
	rec := f.do(t, "GET", "/api/today", "")
	got := decodeInto[struct {
		Ideas []ideas.Idea `json:"ideas"`
	}](t, rec)
	if len(got.Ideas) != 1 || got.Ideas[0].ID != shown.ID {
		t.Fatalf("today ideas = %s", rec.Body.String())
	}
}

// Without schedules (no notification store) nothing is sent and nothing is
// claimed: the start page still lists the follow-up.
func TestIdeaFollowUpsOffWithoutSchedules(t *testing.T) {
	ctx := context.Background()
	f := newIdeaFixture(t)
	idea, _ := f.ideas.Create(ctx, testUser.ID, ideas.StateOpen, ideas.Input{Title: "t", Reason: "r", RemindOn: ideas.DateOf("2026-01-05")}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s := newServer(Deps{Thread: f.chat, Usage: stubUsageStore{}, Ideas: f.ideas})
	s.now = func() time.Time { return time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC) }
	s.sendIdeaFollowUps(ctx)
	got, _ := f.ideas.GetAny(ctx, testUser.ID, idea.ID)
	if got.RemindedAt != nil {
		t.Fatal("claimed without a way to notify")
	}
}
