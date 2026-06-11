package store

import (
	"path/filepath"
	"testing"
)

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
	if count != 3 {
		t.Errorf("applied migrations = %d, want 3", count)
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
	if count != 3 {
		t.Errorf("applied migrations after re-open = %d, want 3", count)
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
