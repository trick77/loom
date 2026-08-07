package store

import (
	"path/filepath"
	"testing"
)

// TestMigration0025_CapitalizesExistingThreadTitles exercises the real 0025
// migration FILE against rows that predate it — the path a live deployment takes,
// which the fresh-DB suite (threads table empty when 0025 runs) never covers.
//
// It opens a fully-migrated DB, seeds titles in each shape the guard has to tell
// apart, then re-applies the exact bytes of the migration (it is idempotent, so
// running it a second time is the test) and asserts only the plain lowercase ones
// were rewritten.
func TestMigration0025_CapitalizesExistingThreadTitles(t *testing.T) {
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
	// Every thread holds the first prompt as its own user message; the title is
	// what differs. Threads whose title still equals that message are the
	// prompt-derived ones the migration is allowed to rewrite.
	seeded := []struct{ id, title, prompt string }{
		{"t1", "why is the sky blue?", "why is the sky blue?"},
		{"t2", "iPhone battery drains overnight", "iPhone battery drains overnight"},
		{"t3", "Blue Sky Explanation", "why is the sky blue?"},
		{"t4", "42 ways to fold a map", "42 ways to fold a map"},
		// Non-ASCII first letter: SQLite's upper() cannot touch it, so the
		// migration must leave it alone rather than corrupt the bytes.
		{"t5", "über die wolken", "über die wolken"},
		// Renamed by hand — the title is no longer the prompt, so it is the
		// user's own text and stays exactly as they typed it.
		{"t6", "ffmpeg notes", "how do I re-encode a mkv without re-encoding audio?"},
	}
	for _, row := range seeded {
		exec(`INSERT INTO threads (id, user_id, title) VALUES (?,'u1',?)`, row.id, row.title)
		exec(
			`INSERT INTO messages (id, thread_id, user_id, role, content) VALUES (?,?,'u1','user',?)`,
			"m-"+row.id, row.id, row.prompt,
		)
	}

	body, err := migrationsFS.ReadFile("migrations/0025_capitalize_thread_titles.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("apply 0025 against populated db: %v", err)
	}

	want := map[string]string{
		"t1": "Why is the sky blue?",
		"t2": "iPhone battery drains overnight",
		"t3": "Blue Sky Explanation",
		"t4": "42 ways to fold a map",
		"t5": "über die wolken",
		"t6": "ffmpeg notes",
	}
	for id, expected := range want {
		var got string
		if err := db.QueryRow(`SELECT title FROM threads WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("select %s: %v", id, err)
		}
		if got != expected {
			t.Errorf("thread %s title = %q, want %q", id, got, expected)
		}
	}
}
