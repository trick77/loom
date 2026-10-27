package store

import (
	"path/filepath"
	"testing"
)

// 0046 drops a run's item count and labels; its status stays.
func TestMigration0046_runsDropTheirItems(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0045_schedule_run_labels.sql")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`)
	exec(`INSERT INTO schedules (id, user_id, name, prompt, interval_text, cron_template, cron, timezone)
	      VALUES ('s','u','S','p','daily','0 9 * * *','0 9 * * *','UTC')`)
	exec(`INSERT INTO schedule_runs (id, schedule_id, user_id, status, new_count, labels) VALUES ('r','s','u','new',2,'["A","B"]')`)

	if err := migrate(db); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	for _, column := range []string{"labels", "new_count"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('schedule_runs') WHERE name = ?`, column).Scan(&n); err != nil || n != 0 {
			t.Fatalf("schedule_runs.%s present = %d (err %v), want dropped", column, n, err)
		}
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM schedule_runs WHERE id = 'r'`).Scan(&status); err != nil || status != "new" {
		t.Fatalf("run status = %q (err %v), want new", status, err)
	}
}
