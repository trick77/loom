package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func insertTestSchedule(t *testing.T, db *sql.DB, id, userID string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO schedules (id, user_id, name, prompt, interval_text, cron_template, cron, timezone)
VALUES (?, ?, 'n', 'p', 'i', 't', 'c', 'UTC')`, id, userID); err != nil {
		t.Fatal(err)
	}
}

// runThread creates a run thread of the schedule holding the run's prompt and
// its answer.
func runThread(t *testing.T, st *Store, userID, scheduleID string) Thread {
	t.Helper()
	ctx := context.Background()
	thread, err := st.CreateThread(ctx, userID, CreateThreadInput{Title: "Run", ScheduleID: &scheduleID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddMessage(ctx, userID, thread.ID, RoleUser, "the schedule's prompt"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddMessage(ctx, userID, thread.ID, RoleAssistant, "the answer"); err != nil {
		t.Fatal(err)
	}
	return thread
}

func TestReplaceableScheduleThreads(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	bob := insertTestUser(t, db, "bob")
	st := NewStore(db)
	insertTestSchedule(t, db, "s1", alice)
	insertTestSchedule(t, db, "s2", alice)
	insertTestSchedule(t, db, "sb", bob)

	untouched := runThread(t, st, alice, "s1")
	current := runThread(t, st, alice, "s1")
	asked := runThread(t, st, alice, "s1")
	if _, err := st.AddMessage(ctx, alice, asked.ID, RoleUser, "a follow-up"); err != nil {
		t.Fatal(err)
	}
	queued := runThread(t, st, alice, "s1")
	if _, err := st.EnqueueMessage(ctx, alice, queued.ID, "waiting", nil, "send-1"); err != nil {
		t.Fatal(err)
	}
	starred := runThread(t, st, alice, "s1")
	if _, _, err := st.SetThreadStarred(ctx, alice, starred.ID, true); err != nil {
		t.Fatal(err)
	}
	filed := runThread(t, st, alice, "s1")
	project, err := st.CreateProject(ctx, alice, CreateProjectInput{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.UpdateThread(ctx, alice, filed.ID, UpdateThreadInput{ProjectID: ProjectIDUpdate{Set: true, Value: &project.ID}}); err != nil {
		t.Fatal(err)
	}
	shared := runThread(t, st, alice, "s1")
	if _, err := st.CreateShare(ctx, alice, CreateShareInput{ShareID: "share-1", ThreadID: shared.ID, Title: "t", Snapshot: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	runThread(t, st, alice, "s2")
	runThread(t, st, bob, "sb")
	if _, err := st.CreateThread(ctx, alice, CreateThreadInput{Title: "Plain"}); err != nil {
		t.Fatal(err)
	}

	got, err := st.ReplaceableScheduleThreads(ctx, alice, "s1", current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{untouched.ID}) {
		t.Fatalf("replaceable = %v, want only the untouched thread %s", got, untouched.ID)
	}
}

func TestStaleScheduleThreads(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	bob := insertTestUser(t, db, "bob")
	st := NewStore(db)
	insertTestSchedule(t, db, "s1", alice)
	insertTestSchedule(t, db, "sb", bob)

	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	age := func(thread Thread, days int) {
		t.Helper()
		if _, err := db.Exec(`UPDATE threads SET created_at = ? WHERE id = ?`,
			now.AddDate(0, 0, -days).Format(time.DateTime), thread.ID); err != nil {
			t.Fatal(err)
		}
	}
	old := runThread(t, st, alice, "s1")
	age(old, 40)
	oldBob := runThread(t, st, bob, "sb")
	age(oldBob, 31)
	young := runThread(t, st, alice, "s1")
	age(young, 10)
	asked := runThread(t, st, alice, "s1")
	age(asked, 40)
	if _, err := st.AddMessage(ctx, alice, asked.ID, RoleUser, "a follow-up"); err != nil {
		t.Fatal(err)
	}
	plain, _ := st.CreateThread(ctx, alice, CreateThreadInput{Title: "Plain"})
	age(plain, 40)

	got, err := st.StaleScheduleThreads(ctx, now.AddDate(0, 0, -30), 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []ThreadRef{{UserID: alice, ThreadID: old.ID}, {UserID: bob, ThreadID: oldBob.ID}}
	if !slices.Equal(got, want) {
		t.Fatalf("stale = %v, want %v", got, want)
	}
	if got, _ := st.StaleScheduleThreads(ctx, now.AddDate(0, 0, -30), 1); len(got) != 1 || got[0].ThreadID != old.ID {
		t.Fatalf("stale with limit 1 = %v, want the oldest", got)
	}
}

func TestUntouchedRunThreadsReportsAStoreFailure(t *testing.T) {
	db := openTestDB(t)
	st := NewStore(db)
	_ = db.Close()
	if _, err := st.ReplaceableScheduleThreads(context.Background(), "u", "s", "t"); err == nil {
		t.Fatal("ReplaceableScheduleThreads on a closed database returned no error")
	}
	if _, err := st.StaleScheduleThreads(context.Background(), time.Now(), 1); err == nil {
		t.Fatal("StaleScheduleThreads on a closed database returned no error")
	}
}
