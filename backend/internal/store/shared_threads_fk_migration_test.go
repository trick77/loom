package store

import (
	"path/filepath"
	"testing"
)

// shared_threads referenced threads(id) alone, so the database accepted a
// share row whose user_id was not the thread's owner; ownership was enforced
// only in the handler. The rebuilt table binds (user_id, thread_id) to
// threads(user_id, id) like every other child table.
func TestSharedThreads_foreignKeyIsUserScoped(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	for _, stmt := range []string{
		`INSERT INTO users (id, oidc_subject, username, role) VALUES ('owner','s1','owner','user')`,
		`INSERT INTO users (id, oidc_subject, username, role) VALUES ('other','s2','other','user')`,
		`INSERT INTO threads (id, user_id, title) VALUES ('t','owner','T')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if _, err := db.Exec(
		`INSERT INTO shared_threads (id, share_id, thread_id, user_id, title, snapshot) VALUES ('s','pub','t','other','T','{}')`,
	); err == nil {
		t.Fatal("share row for another user's thread inserted, want FK error")
	}
	if _, err := db.Exec(
		`INSERT INTO shared_threads (id, share_id, thread_id, user_id, title, snapshot) VALUES ('s','pub','t','owner','T','{}')`,
	); err != nil {
		t.Fatalf("owner's share row rejected: %v", err)
	}

	// Deleting the thread still cascades the share away, and the index survives.
	if _, err := db.Exec(`DELETE FROM threads WHERE id = 't'`); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM shared_threads`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("shares after thread delete = %d (err %v), want 0", n, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_shared_threads_user'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("idx_shared_threads_user present = %d (err %v), want 1", n, err)
	}
}
