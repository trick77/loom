package store

import (
	"path/filepath"
	"testing"
)

// TestMigration0021_BackfillsPopulatedCorpus exercises the real 0021 migration
// FILE against a DB that already contains messages — the path a live deployment
// takes, which the fresh-DB suite (table empty when 0021 runs) never covers.
//
// It opens a fully-migrated DB, seeds users/threads/messages, then drops the
// message_fts table + triggers and re-applies the exact bytes of
// migrations/0021_message_search.sql, asserting the backfill reindexes only the
// user/assistant rows and that a MATCH query then returns them.
func TestMigration0021_BackfillsPopulatedCorpus(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u1','s1','alice','user')`)
	exec(`INSERT INTO threads (id, user_id, title) VALUES ('t1','u1','Pre-existing')`)
	exec(`INSERT INTO messages (id, thread_id, user_id, role, content) VALUES ('m1','t1','u1','user','platypus migration question')`)
	exec(`INSERT INTO messages (id, thread_id, user_id, role, content) VALUES ('m2','t1','u1','assistant','the platypus answer lives here')`)
	exec(`INSERT INTO messages (id, thread_id, user_id, role, content) VALUES ('m3','t1','u1','tool','platypus tool blob stays out')`)

	// Simulate a pre-0021 database that already holds these rows: drop the FTS
	// objects so re-applying the migration runs its backfill against real data.
	exec(`DROP TRIGGER message_fts_ai`)
	exec(`DROP TRIGGER message_fts_ad`)
	exec(`DROP TRIGGER message_fts_au`)
	exec(`DROP TABLE message_fts`)

	body, err := migrationsFS.ReadFile("migrations/0021_message_search.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("apply 0021 against populated db: %v", err)
	}

	var indexed, want int
	if err := db.QueryRow(`SELECT count(*) FROM message_fts`).Scan(&indexed); err != nil {
		t.Fatalf("count fts: %v", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM messages WHERE role IN ('user','assistant')`).Scan(&want); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if indexed != want {
		t.Fatalf("backfill indexed %d rows, want %d (user+assistant only)", indexed, want)
	}
	if want != 2 {
		t.Fatalf("expected 2 user/assistant rows, got %d", want)
	}

	var matched int
	if err := db.QueryRow(`SELECT count(*) FROM message_fts WHERE message_fts MATCH 'platypus'`).Scan(&matched); err != nil {
		t.Fatalf("match query: %v", err)
	}
	if matched != 2 {
		t.Fatalf("MATCH 'platypus' returned %d rows, want 2", matched)
	}
}
