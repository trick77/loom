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
