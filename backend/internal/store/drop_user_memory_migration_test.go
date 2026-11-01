package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// 0048 refuses to drop a blob that was never converted into facts: an upgrade
// that skips 0.2.14 runs 0047 and 0048 before any memory sweep.
func TestMigration0048_refusesUnconvertedUserMemory(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0047_user_facts.sql")
	for _, q := range []string{
		`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`,
		`INSERT INTO user_memory (user_id, content) VALUES ('u', '- likes tea')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	err := migrate(db)
	if err == nil || !strings.Contains(err.Error(), "upgrade_through_0_2_14_first") {
		t.Fatalf("migrate() error = %v, want the guard naming the upgrade path", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM user_memory`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("user_memory rows = %d (err %v), want the blob kept", n, err)
	}
}

// 0048 drops the old user memory blob once every user with one has a fact
// cursor (converted); an empty blob needs none. Nothing else is touched.
func TestMigration0048_dropsUserMemory(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0047_user_facts.sql")
	for _, q := range []string{
		`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user'), ('e','s2','e','user')`,
		`INSERT INTO user_memory (user_id, content) VALUES ('u', '- likes tea'), ('e', '  ')`,
		`INSERT INTO user_fact_cursor (user_id) VALUES ('u')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
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
