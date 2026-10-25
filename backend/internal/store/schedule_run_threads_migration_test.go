package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// Before 0044 a schedule kept one shared thread every run wrote into. The
// migration moves the thread onto each run (the thread of its answer, else
// the schedule's) and drops the schedule's thread and the branch links.
func TestMigration0044_runsCarryTheirThread(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0043_schedules.sql")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`)
	exec(`INSERT INTO threads (id, user_id, title) VALUES ('feed','u','Feed'), ('other','u','Other')`)
	exec(`INSERT INTO threads (id, user_id, title, branched_from_thread_id) VALUES ('branch','u','Branch','feed')`)
	exec(`INSERT INTO messages (id, thread_id, user_id, role, content) VALUES ('m','other','u','assistant','hi')`)
	exec(`INSERT INTO schedules (id, user_id, name, prompt, interval_text, cron_template, cron, timezone, thread_id)
	      VALUES ('s','u','S','p','daily','0 9 * * *','0 9 * * *','UTC','feed')`)
	exec(`INSERT INTO schedule_runs (id, schedule_id, user_id, message_id) VALUES ('answered','s','u','m'), ('failed','s','u',NULL)`)

	if err := migrate(db); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	for run, want := range map[string]string{"answered": "other", "failed": "feed"} {
		var got sql.NullString
		if err := db.QueryRow(`SELECT thread_id FROM schedule_runs WHERE id = ?`, run).Scan(&got); err != nil || got.String != want {
			t.Fatalf("run %s thread = %v (err %v), want %s", run, got, err, want)
		}
	}
	for thread, want := range map[string]string{"feed": "s", "other": ""} {
		var got sql.NullString
		if err := db.QueryRow(`SELECT schedule_id FROM threads WHERE id = ?`, thread).Scan(&got); err != nil || got.String != want {
			t.Fatalf("thread %s schedule = %v (err %v), want %q", thread, got, err, want)
		}
	}
	for table, column := range map[string]string{"schedules": "thread_id", "threads": "branched_from_thread_id"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s.%s present = %d (err %v), want dropped", table, column, n, err)
		}
	}
	exec(`DELETE FROM threads WHERE id = 'feed'`)
	var got sql.NullString
	if err := db.QueryRow(`SELECT thread_id FROM schedule_runs WHERE id = 'failed'`).Scan(&got); err != nil || got.Valid {
		t.Fatalf("run thread after thread delete = %v (err %v), want NULL", got, err)
	}
}
