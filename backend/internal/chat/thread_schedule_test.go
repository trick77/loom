package chat

import (
	"context"
	"testing"
)

func TestThreadScheduleID(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	st := NewStore(db)
	if _, err := db.Exec(`INSERT INTO schedules (id, user_id, name, prompt, interval_text, cron_template, cron, timezone)
VALUES ('s1', ?, 'n', 'p', 'i', 't', 'c', 'UTC')`, alice); err != nil {
		t.Fatal(err)
	}
	scheduleID := "s1"
	run, err := st.CreateThread(ctx, alice, CreateThreadInput{Title: "Cinema scout", ScheduleID: &scheduleID})
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := st.CreateThread(ctx, alice, CreateThreadInput{Title: "Plain"})

	if run.ScheduleID == nil || *run.ScheduleID != "s1" {
		t.Fatalf("run thread ScheduleID = %v, want s1", run.ScheduleID)
	}
	listed, _ := st.ListThreads(ctx, alice, ListThreadsOptions{})
	for _, th := range listed {
		if (th.ID == run.ID) != (th.ScheduleID != nil) {
			t.Fatalf("listed %s ScheduleID = %v", th.Title, th.ScheduleID)
		}
	}
	if got, _, _ := st.GetThread(ctx, alice, plain.ID); got.ScheduleID != nil {
		t.Fatalf("plain thread ScheduleID = %v, want nil", got.ScheduleID)
	}
	// Deleting the schedule keeps the thread, unlinked.
	if _, err := db.Exec(`DELETE FROM schedules WHERE id = 's1'`); err != nil {
		t.Fatal(err)
	}
	if got, found, _ := st.GetThread(ctx, alice, run.ID); !found || got.ScheduleID != nil {
		t.Fatalf("thread after schedule delete = %+v (found %v)", got, found)
	}
}

func TestThreadScheduleReplied(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	st := NewStore(db)
	insertTestSchedule(t, db, "s1", alice)

	fresh := runThread(t, st, alice, "s1")
	replied := runThread(t, st, alice, "s1")
	if _, err := st.AddMessage(ctx, alice, replied.ID, RoleUser, "a follow-up about tomatoes"); err != nil {
		t.Fatal(err)
	}
	plain, _ := st.CreateThread(ctx, alice, CreateThreadInput{Title: "Plain"})
	for _, content := range []string{"first tomatoes", "second tomatoes"} {
		if _, err := st.AddMessage(ctx, alice, plain.ID, RoleUser, content); err != nil {
			t.Fatal(err)
		}
	}
	// Only a run thread the user wrote in after the run's prompt counts.
	want := map[string]bool{fresh.ID: false, replied.ID: true, plain.ID: false}

	for id, w := range want {
		got, _, err := st.GetThread(ctx, alice, id)
		if err != nil || got.ScheduleReplied != w {
			t.Fatalf("GetThread(%s).ScheduleReplied = %v (err %v), want %v", id, got.ScheduleReplied, err, w)
		}
	}
	listed, err := st.ListThreads(ctx, alice, ListThreadsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, th := range listed {
		if th.ScheduleReplied != want[th.ID] {
			t.Fatalf("listed %s ScheduleReplied = %v, want %v", th.ID, th.ScheduleReplied, want[th.ID])
		}
	}
	hits, err := st.SearchThreadsByContent(ctx, alice, "tomatoes", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("search hits = %d, want 2", len(hits))
	}
	for _, hit := range hits {
		if hit.Thread.ScheduleReplied != want[hit.Thread.ID] {
			t.Fatalf("search hit %s ScheduleReplied = %v, want %v", hit.Thread.ID, hit.Thread.ScheduleReplied, want[hit.Thread.ID])
		}
	}
}
