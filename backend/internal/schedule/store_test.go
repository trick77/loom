package schedule_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/schedule"
	"github.com/trick77/loom/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "schedule_test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, id := range []string{"u1", "u2"} {
		if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES (?, ?, ?, 'user')`, id, "sub-"+id, id); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}
	return db
}

func seedThread(t *testing.T, db *sql.DB, userID, id string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO threads (id, user_id, title) VALUES (?, ?, 't')`, id, userID); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
}

func newSchedule(userID, name string, next *time.Time) schedule.Schedule {
	return schedule.Schedule{
		UserID: userID, Name: name, Prompt: "find films", IntervalText: "once a week",
		CronTemplate: "~ 7~22 * * ~", Cron: "37 14 * * 4", Timezone: "Europe/Zurich", Enabled: true, NextRunAt: next,
	}
}

func TestScheduleCRUDIsScopedByUser(t *testing.T) {
	ctx := context.Background()
	st := schedule.NewStore(openDB(t))
	next := time.Date(2026, 10, 29, 13, 37, 0, 0, time.UTC)
	created, err := st.CreateSchedule(ctx, newSchedule("u1", "Cinema scout", &next))
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}
	if created.ID == "" || !created.NextRunAt.Equal(next) || !created.Enabled || created.Running {
		t.Fatalf("created = %+v", created)
	}
	if _, err := st.GetSchedule(ctx, "u2", created.ID); !errors.Is(err, schedule.ErrNotFound) {
		t.Fatalf("another user's GetSchedule err = %v, want ErrNotFound", err)
	}
	if list, _ := st.ListSchedules(ctx, "u2"); len(list) != 0 {
		t.Fatalf("u2 sees %d schedules", len(list))
	}
	created.Name = "Renamed"
	created.Enabled = false
	created.DisabledReason = "timeout"
	saved, err := st.SaveSchedule(ctx, created)
	if err != nil || saved.Name != "Renamed" || saved.Enabled || saved.DisabledReason != "timeout" {
		t.Fatalf("SaveSchedule = %+v, %v", saved, err)
	}
	other := created
	other.UserID = "u2"
	if _, err := st.SaveSchedule(ctx, other); !errors.Is(err, schedule.ErrNotFound) {
		t.Fatalf("SaveSchedule as u2 err = %v", err)
	}
	if err := st.DeleteSchedule(ctx, "u2", created.ID); !errors.Is(err, schedule.ErrNotFound) {
		t.Fatalf("DeleteSchedule as u2 err = %v", err)
	}
	if err := st.DeleteSchedule(ctx, "u1", created.ID); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	if _, err := st.GetSchedule(ctx, "u1", created.ID); !errors.Is(err, schedule.ErrNotFound) {
		t.Fatalf("deleted schedule still found: %v", err)
	}
}

func TestCreateScheduleRefusesPastTheLimit(t *testing.T) {
	ctx := context.Background()
	st := schedule.NewStore(openDB(t))
	for i := 0; i < schedule.MaxSchedulesPerUser; i++ {
		if _, err := st.CreateSchedule(ctx, newSchedule("u1", fmt.Sprint(i), nil)); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if _, err := st.CreateSchedule(ctx, newSchedule("u1", "one too many", nil)); !errors.Is(err, schedule.ErrLimitReached) {
		t.Fatalf("err = %v, want ErrLimitReached", err)
	}
	if _, err := st.CreateSchedule(ctx, newSchedule("u2", "other user", nil)); err != nil {
		t.Fatalf("the limit is per user: %v", err)
	}
}

func TestClaimIsExclusiveAndDueListsOnlyIdleEnabled(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	st := schedule.NewStore(db)
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	due, _ := st.CreateSchedule(ctx, newSchedule("u1", "due", &past))
	_, _ = st.CreateSchedule(ctx, newSchedule("u1", "later", &future))
	off := newSchedule("u2", "off", &past)
	off.Enabled = false
	_, _ = st.CreateSchedule(ctx, off)

	list, err := st.DueSchedules(ctx, now, 10)
	if err != nil || len(list) != 1 || list[0].ID != due.ID {
		t.Fatalf("DueSchedules = %+v, %v", list, err)
	}
	next := now.Add(7 * 24 * time.Hour)
	ok, err := st.ClaimDue(ctx, "u1", due.ID, now, &next)
	if err != nil || !ok {
		t.Fatalf("first claim = %v, %v", ok, err)
	}
	if ok, _ := st.Claim(ctx, "u1", due.ID, now); ok {
		t.Fatal("second claim succeeded while the first holds it")
	}
	if list, _ := st.DueSchedules(ctx, now.Add(30*24*time.Hour), 10); len(list) != 1 || list[0].Name != "later" {
		t.Fatalf("a running schedule is still due: %+v", list)
	}
	got, _ := st.GetSchedule(ctx, "u1", due.ID)
	if !got.Running || !got.NextRunAt.Equal(next) {
		t.Fatalf("claimed schedule = %+v", got)
	}
	if err := st.Release(ctx, "u1", due.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.Claim(ctx, "u1", due.ID, now); !ok {
		t.Fatal("claim after release failed")
	}
	got, _ = st.GetSchedule(ctx, "u1", due.ID)
	if !got.NextRunAt.Equal(next) {
		t.Fatalf("Claim moved the next run: %v", got.NextRunAt)
	}
	runID, _ := st.StartRun(ctx, "u1", due.ID, now)
	if err := st.ResetInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetSchedule(ctx, "u1", due.ID)
	if got.Running {
		t.Fatal("ResetInterrupted left the claim")
	}
	runs, _ := st.ListRuns(ctx, "u1", due.ID, 5)
	if len(runs) != 1 || runs[0].ID != runID || runs[0].Status != schedule.StatusFailed || runs[0].FinishedAt == nil {
		t.Fatalf("interrupted run = %+v", runs)
	}
}

func TestClaimDueWithoutANextRunLeavesNoneDue(t *testing.T) {
	ctx := context.Background()
	st := schedule.NewStore(openDB(t))
	now := time.Date(2026, 10, 29, 14, 0, 0, 0, time.UTC)
	past := now.Add(-time.Minute)
	sch, _ := st.CreateSchedule(ctx, newSchedule("u1", "broken", &past))
	if ok, err := st.ClaimDue(ctx, "u1", sch.ID, now, nil); err != nil || !ok {
		t.Fatalf("ClaimDue = %v, %v", ok, err)
	}
	_ = st.Release(ctx, "u1", sch.ID)
	if due, _ := st.DueSchedules(ctx, now.Add(time.Hour), 10); len(due) != 0 {
		t.Fatalf("a schedule without a next run is still due: %+v", due)
	}
}

func TestDisableAndThreadLink(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	st := schedule.NewStore(db)
	now := time.Now().UTC()
	sch, _ := st.CreateSchedule(ctx, newSchedule("u1", "s", &now))
	seedThread(t, db, "u1", "th1")
	if err := st.SetThread(ctx, "u1", sch.ID, "th1"); err != nil {
		t.Fatal(err)
	}
	found, ok, err := st.ScheduleForThread(ctx, "u1", "th1")
	if err != nil || !ok || found.ID != sch.ID {
		t.Fatalf("ScheduleForThread = %+v, %v, %v", found, ok, err)
	}
	if _, ok, _ := st.ScheduleForThread(ctx, "u2", "th1"); ok {
		t.Fatal("another user found the schedule by thread")
	}
	if err := st.Disable(ctx, "u1", sch.ID, schedule.DisabledTimeout, now); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetSchedule(ctx, "u1", sch.ID)
	if got.Enabled || got.DisabledReason != schedule.DisabledTimeout || got.DisabledAt == nil || got.NextRunAt != nil {
		t.Fatalf("disabled = %+v", got)
	}
	// Deleting the thread unlinks the schedule.
	if _, err := db.Exec(`DELETE FROM threads WHERE id = 'th1'`); err != nil {
		t.Fatal(err)
	}
	got, _ = st.GetSchedule(ctx, "u1", sch.ID)
	if got.ThreadID != nil {
		t.Fatalf("thread id after delete = %v", *got.ThreadID)
	}
}

func TestRunsFindingsAndLabels(t *testing.T) {
	ctx := context.Background()
	st := schedule.NewStore(openDB(t))
	now := time.Date(2026, 10, 22, 12, 37, 0, 0, time.UTC)
	sch, _ := st.CreateSchedule(ctx, newSchedule("u1", "s", nil))
	first, _ := st.StartRun(ctx, "u1", sch.ID, now.AddDate(0, 0, -7))
	fresh, err := st.AddFindings(ctx, "u1", sch.ID, first, []schedule.Finding{{Key: "supergirl", Label: "Supergirl"}}, now.AddDate(0, 0, -7))
	if err != nil || len(fresh) != 1 {
		t.Fatalf("AddFindings = %v, %v", fresh, err)
	}
	_ = st.FinishRun(ctx, "u1", schedule.Run{ID: first, Status: schedule.StatusNew, NewCount: 1}, now.AddDate(0, 0, -7))

	second, _ := st.StartRun(ctx, "u1", sch.ID, now)
	fresh, err = st.AddFindings(ctx, "u1", sch.ID, second, []schedule.Finding{
		{Key: "supergirl", Label: "Supergirl"},
		{Key: "dune-3", Label: "Dune: Part Three", SourceURL: "https://arena.ch/x"},
		{Key: "avengers", Label: "Avengers: Doomsday"},
	}, now)
	if err != nil || len(fresh) != 2 || fresh[0].Key != "dune-3" {
		t.Fatalf("second AddFindings = %+v, %v (an item already reported must not be new)", fresh, err)
	}
	_ = st.FinishRun(ctx, "u1", schedule.Run{ID: second, Status: schedule.StatusNew, NewCount: 2, Tokens: 41000, Rounds: 4, DurationMS: 108000}, now)

	runs, err := st.ListRuns(ctx, "u1", sch.ID, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("ListRuns = %+v, %v", runs, err)
	}
	if runs[0].ID != second || strings.Join(runs[0].NewLabels, ",") != "Dune: Part Three,Avengers: Doomsday" || runs[0].Tokens != 41000 {
		t.Fatalf("latest run = %+v", runs[0])
	}
	if strings.Join(runs[1].NewLabels, ",") != "Supergirl" {
		t.Fatalf("older run labels = %v", runs[1].NewLabels)
	}
	last, err := st.LastRuns(ctx, "u1")
	if err != nil || last[sch.ID].ID != second {
		t.Fatalf("LastRuns = %+v, %v", last, err)
	}
	if other, _ := st.ListRuns(ctx, "u2", sch.ID, 10); len(other) != 0 {
		t.Fatal("another user listed the runs")
	}
	findings, _ := st.ListFindings(ctx, "u1", sch.ID)
	if len(findings) != 3 {
		t.Fatalf("ListFindings = %+v", findings)
	}
	if err := st.DeleteFindings(ctx, "u1", sch.ID, "supergirl"); err != nil {
		t.Fatal(err)
	}
	if findings, _ = st.ListFindings(ctx, "u1", sch.ID); len(findings) != 2 {
		t.Fatalf("after forgetting one: %+v", findings)
	}
	if err := st.DeleteFindings(ctx, "u1", sch.ID, ""); err != nil {
		t.Fatal(err)
	}
	if findings, _ = st.ListFindings(ctx, "u1", sch.ID); len(findings) != 0 {
		t.Fatalf("after forgetting all: %+v", findings)
	}
}

func TestNotifications(t *testing.T) {
	ctx := context.Background()
	st := schedule.NewStore(openDB(t))
	for _, title := range []string{"one", "two"} {
		if err := st.CreateNotification(ctx, "u1", schedule.Notification{Kind: schedule.KindScheduleRun, Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	_ = st.CreateNotification(ctx, "u2", schedule.Notification{Kind: schedule.KindScheduleDisabled, Title: "other"})
	list, err := st.ListNotifications(ctx, "u1", 10)
	if err != nil || len(list) != 2 || list[0].Title != "two" {
		t.Fatalf("ListNotifications = %+v, %v", list, err)
	}
	if n, _ := st.UnreadCount(ctx, "u1"); n != 2 {
		t.Fatalf("unread = %d", n)
	}
	if err := st.MarkRead(ctx, "u1", list[0].ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.UnreadCount(ctx, "u1"); n != 1 {
		t.Fatalf("unread after one read = %d", n)
	}
	// Marking another user's notification does nothing.
	other, _ := st.ListNotifications(ctx, "u2", 10)
	_ = st.MarkRead(ctx, "u1", other[0].ID)
	if n, _ := st.UnreadCount(ctx, "u2"); n != 1 {
		t.Fatal("u1 marked u2's notification read")
	}
	_ = st.MarkRead(ctx, "u1", "")
	if n, _ := st.UnreadCount(ctx, "u1"); n != 0 {
		t.Fatalf("unread after all read = %d", n)
	}
	if err := st.Prune(ctx, time.Now().Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if list, _ = st.ListNotifications(ctx, "u1", 10); len(list) != 0 {
		t.Fatalf("old notifications survived Prune: %+v", list)
	}
}
