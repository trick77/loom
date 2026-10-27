package store

import (
	"path/filepath"
	"testing"
)

// 0045 drops the schedule's record of what it reported; each run keeps the
// labels it reported, in order, and a run that reported nothing an empty list.
func TestMigration0045_runsKeepTheirLabels(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0044_schedule_run_threads.sql")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`)
	exec(`INSERT INTO schedules (id, user_id, name, prompt, interval_text, cron_template, cron, timezone)
	      VALUES ('s','u','S','p','daily','0 9 * * *','0 9 * * *','UTC')`)
	exec(`INSERT INTO schedule_runs (id, schedule_id, user_id) VALUES ('news','s','u'), ('quiet','s','u')`)
	exec(`INSERT INTO schedule_findings (schedule_id, user_id, item_key, label, run_id, first_seen_at) VALUES
	      ('s','u','b','Second','news','2026-10-20 09:00:01'),
	      ('s','u','a','First','news','2026-10-20 09:00:00'),
	      ('s','u','orphan','Orphan',NULL,'2026-10-19 09:00:00')`)

	if err := migrate(db); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	for run, want := range map[string]string{"news": `["First","Second"]`, "quiet": `[]`} {
		var got string
		if err := db.QueryRow(`SELECT labels FROM schedule_runs WHERE id = ?`, run).Scan(&got); err != nil || got != want {
			t.Fatalf("run %s labels = %s (err %v), want %s", run, got, err, want)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'schedule_findings'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("schedule_findings present = %d (err %v), want dropped", n, err)
	}
}
