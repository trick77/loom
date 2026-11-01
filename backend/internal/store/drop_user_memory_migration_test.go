package store

import (
	"path/filepath"
	"testing"
)

// 0048 drops the old user memory blob once every user is on facts; its rows
// go with it and nothing else is touched.
func TestMigration0048_dropsUserMemory(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0047_user_facts.sql")
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO user_memory (user_id, content) VALUES ('u', '- likes tea')`); err != nil {
		t.Fatalf("insert user memory: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'user_memory'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("user_memory tables = %d (err %v), want dropped", n, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE id = 'u'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("users = %d (err %v), want the user kept", n, err)
	}
}
