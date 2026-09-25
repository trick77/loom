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

// A legacy share row owned by someone other than the thread's owner, which the
// old single-column key accepted, must not stop the rebuild: it is dropped.
func TestMigration0028_dropsLegacyMismatchedShareRows(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0027_cost_nano_usd.sql")
	for _, stmt := range []string{
		`INSERT INTO users (id, oidc_subject, username, role) VALUES ('owner','s1','owner','user')`,
		`INSERT INTO users (id, oidc_subject, username, role) VALUES ('other','s2','other','user')`,
		`INSERT INTO threads (id, user_id, title) VALUES ('t','owner','T')`,
		`INSERT INTO threads (id, user_id, title) VALUES ('t2','owner','T2')`,
		`INSERT INTO shared_threads (id, share_id, thread_id, user_id, title, snapshot) VALUES ('good','pub1','t','owner','T','{}')`,
		`INSERT INTO shared_threads (id, share_id, thread_id, user_id, title, snapshot) VALUES ('bad','pub2','t2','other','T2','{}')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var ids []string
	rows, err := db.Query(`SELECT id FROM shared_threads ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ids = append(ids, id)
	}
	if len(ids) != 1 || ids[0] != "good" {
		t.Fatalf("shares after migration = %v, want [good]", ids)
	}
}
