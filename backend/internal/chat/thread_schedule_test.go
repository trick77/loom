package chat

import (
	"context"
	"testing"
)

func TestThreadScheduleIDComesFromItsRun(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	st := NewStore(db)
	run, _ := st.CreateThread(ctx, alice, CreateThreadInput{Title: "Cinema scout"})
	plain, _ := st.CreateThread(ctx, alice, CreateThreadInput{Title: "Plain"})
	if _, err := db.Exec(`INSERT INTO schedules (id, user_id, name, prompt, interval_text, cron_template, cron, timezone)
VALUES ('s1', ?, 'n', 'p', 'i', 't', 'c', 'UTC')`, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schedule_runs (id, schedule_id, user_id, thread_id) VALUES ('r1', 's1', ?, ?)`, alice, run.ID); err != nil {
		t.Fatal(err)
	}

	got, _, _ := st.GetThread(ctx, alice, run.ID)
	if got.ScheduleID == nil || *got.ScheduleID != "s1" {
		t.Fatalf("run thread ScheduleID = %v, want s1", got.ScheduleID)
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
}
