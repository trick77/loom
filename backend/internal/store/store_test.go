package store

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// embeddedMigrationCount returns how many migration files are embedded, so the
// migration-count assertions stay correct as migrations are added.
func embeddedMigrationCount(t *testing.T) int {
	t.Helper()
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count
}

func TestOpen_runsMigrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"settings", "users", "sessions", "projects", "threads", "messages", "artifacts"} {
		var name string
		err = db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
			table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("%s table missing: %v", table, err)
		}
	}

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if want := embeddedMigrationCount(t); count != want {
		t.Errorf("applied migrations = %d, want %d", count, want)
	}
}

func TestOpen_migrationsAreIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open() error: %v", err)
	}
	db1.Close()

	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open() error: %v", err)
	}
	defer db2.Close()

	var count int
	if err := db2.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if want := embeddedMigrationCount(t); count != want {
		t.Errorf("applied migrations after re-open = %d, want %d", count, want)
	}
}

func TestMigrate_truncatesExistingMemoryToThreeThousandRunes(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
CREATE TABLE schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE project_memory (
    user_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    source_message_count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, project_id)
);
CREATE TABLE user_memory (
    user_id TEXT NOT NULL PRIMARY KEY,
    content TEXT NOT NULL DEFAULT '',
    source_message_count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);`); err != nil {
		t.Fatalf("create old memory schema: %v", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "0009_memory_length_caps.sql" {
			continue
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, entry.Name()); err != nil {
			t.Fatalf("mark migration %s applied: %v", entry.Name(), err)
		}
	}

	oversizedUser := strings.Repeat("é", 3200)
	oversizedProject := strings.Repeat("ø", 3200)
	if _, err := db.Exec(`INSERT INTO user_memory (user_id, content) VALUES ('alice', ?)`, oversizedUser); err != nil {
		t.Fatalf("insert oversized user memory: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO project_memory (user_id, project_id, content) VALUES ('alice', 'project', ?)`, oversizedProject); err != nil {
		t.Fatalf("insert oversized project memory: %v", err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate() error: %v", err)
	}

	var userRunes, projectRunes int
	if err := db.QueryRow(`SELECT length(content) FROM user_memory WHERE user_id = 'alice'`).Scan(&userRunes); err != nil {
		t.Fatalf("query user memory length: %v", err)
	}
	if err := db.QueryRow(`SELECT length(content) FROM project_memory WHERE user_id = 'alice' AND project_id = 'project'`).Scan(&projectRunes); err != nil {
		t.Fatalf("query project memory length: %v", err)
	}
	if userRunes != 3000 {
		t.Fatalf("user memory length = %d, want 3000", userRunes)
	}
	if projectRunes != 3000 {
		t.Fatalf("project memory length = %d, want 3000", projectRunes)
	}
}

func TestOpen_chatSchemaRejectsCrossUserRelationships(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	insertTestUser := func(id string) {
		t.Helper()
		_, err := db.Exec(
			`INSERT INTO users (id, oidc_subject, username, role) VALUES (?, ?, ?, 'user')`,
			id,
			"subject-"+id,
			id,
		)
		if err != nil {
			t.Fatalf("insert user %s: %v", id, err)
		}
	}

	insertTestUser("alice")
	insertTestUser("bob")

	if _, err := db.Exec(
		`INSERT INTO projects (id, user_id, name) VALUES ('bob-project', 'bob', 'Bob Project')`,
	); err != nil {
		t.Fatalf("insert bob project: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO threads (id, user_id, project_id, title) VALUES ('alice-thread-cross-project', 'alice', 'bob-project', 'Cross Project')`,
	); err == nil {
		t.Fatal("insert thread with alice user_id and bob project_id succeeded, want foreign key error")
	}

	if _, err := db.Exec(
		`INSERT INTO threads (id, user_id, title) VALUES ('bob-thread', 'bob', 'Bob Thread')`,
	); err != nil {
		t.Fatalf("insert bob thread: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO messages (id, thread_id, user_id, role, content) VALUES ('alice-message-cross-thread', 'bob-thread', 'alice', 'user', 'hello')`,
	); err == nil {
		t.Fatal("insert message with alice user_id and bob thread_id succeeded, want foreign key error")
	}
}
