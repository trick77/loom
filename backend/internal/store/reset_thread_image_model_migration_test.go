package store

import (
	"path/filepath"
	"testing"
)

// TestMigration0026_ClearsThreadImageModel exercises the real 0026 migration FILE
// against rows that predate it — the path a live deployment takes, which the
// fresh-DB suite (threads table empty when 0026 runs) never covers. That gap is
// exactly how a first draft of this migration shipped a `SET image_model = NULL`
// that trips the NOT NULL constraint from 0015 and blocks startup on every
// database holding at least one thread.
//
// It opens a fully-migrated DB, seeds threads locked to the old BFL model ids,
// then re-applies the exact bytes of the migration (it is idempotent, so running
// it a second time is the test) and asserts the lock is cleared to the '' sentinel
// SetThreadImageModelIfEmpty matches on.
func TestMigration0026_ClearsThreadImageModel(t *testing.T) {
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
	seeded := map[string]string{
		// Locked to the old default and the old typography model: both are BFL
		// ids that would 404 on fal.
		"t1": "flux-2-klein-4b",
		"t2": "flux-2-flex",
		// Never generated an image, so already unlocked.
		"t3": "",
	}
	for id, model := range seeded {
		exec(`INSERT INTO threads (id, user_id, title, image_model) VALUES (?,'u1','t',?)`, id, model)
	}

	body, err := migrationsFS.ReadFile("migrations/0026_reset_thread_image_model.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("apply 0026 against populated db: %v", err)
	}

	for id := range seeded {
		var got string
		if err := db.QueryRow(`SELECT image_model FROM threads WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("select %s: %v", id, err)
		}
		if got != "" {
			t.Errorf("thread %s image_model = %q, want the empty unlocked sentinel", id, got)
		}
	}

	// The point of clearing the column is that the next image re-locks the
	// thread, so prove the set-if-empty path actually takes.
	exec(`UPDATE threads SET image_model = 'fal-ai/flux-2-pro' WHERE id = 't1' AND image_model = ''`)
	var relocked string
	if err := db.QueryRow(`SELECT image_model FROM threads WHERE id = 't1'`).Scan(&relocked); err != nil {
		t.Fatalf("select t1: %v", err)
	}
	if relocked != "fal-ai/flux-2-pro" {
		t.Errorf("image_model after re-lock = %q, want fal-ai/flux-2-pro", relocked)
	}
}
