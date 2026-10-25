package store

import (
	"path/filepath"
	"testing"
)

// 0026 reset every 'auto' response language to the empty string but left the
// column's DEFAULT 'auto', so a row inserted without the column still read as
// the removed value. The rebuild makes empty the default. It drops a table every
// per-user table cascades from, so this is also the test that the file's
// foreign_keys = OFF keeps their rows.
func TestMigration0042_usersResponseLanguageDefaultsToUnset(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0041_drop_sessions.sql")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, oidc_subject, username, email, role, response_language, last_seen_at)
	      VALUES ('u','s','u','u@example.com','admin','de','2026-01-01 00:00:00')`)
	exec(`INSERT INTO user_usage_totals (user_id, total_tokens) VALUES ('u', 7)`)
	exec(`INSERT INTO user_memory (user_id, content) VALUES ('u', 'likes tea')`)

	if err := migrate(db); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	var email, role, lang, seen string
	if err := db.QueryRow(`SELECT email, role, response_language, last_seen_at FROM users WHERE id = 'u'`).
		Scan(&email, &role, &lang, &seen); err != nil {
		t.Fatalf("user after rebuild: %v", err)
	}
	if email != "u@example.com" || role != "admin" || lang != "de" || seen != "2026-01-01 00:00:00" {
		t.Fatalf("user after rebuild = %q %q %q %q, want every column kept", email, role, lang, seen)
	}
	var tokens int
	if err := db.QueryRow(`SELECT total_tokens FROM user_usage_totals WHERE user_id = 'u'`).Scan(&tokens); err != nil || tokens != 7 {
		t.Fatalf("usage after rebuild = %d (err %v), want 7: the rebuild cascaded", tokens, err)
	}
	var memory string
	if err := db.QueryRow(`SELECT content FROM user_memory WHERE user_id = 'u'`).Scan(&memory); err != nil || memory != "likes tea" {
		t.Fatalf("memory after rebuild = %q (err %v), want kept", memory, err)
	}

	exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('v','s2','v','user')`)
	if err := db.QueryRow(`SELECT response_language FROM users WHERE id = 'v'`).Scan(&lang); err != nil || lang != "" {
		t.Fatalf("default response_language = %q (err %v), want ''", lang, err)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_users_role'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("idx_users_role present = %d (err %v), want 1", n, err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('w','s3','w','root')`); err == nil {
		t.Fatal("role 'root' accepted: the CHECK was lost")
	}
	if _, err := db.Exec(`INSERT INTO user_memory (user_id) VALUES ('missing')`); err == nil {
		t.Fatal("memory for a missing user inserted: foreign keys were left off")
	}
}
