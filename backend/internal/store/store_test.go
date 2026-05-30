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

	var name string
	err = db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='settings'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("settings table missing: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query: %v", err)
	}
	if count != 1 {
		t.Errorf("applied migrations = %d, want 1", count)
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
	if count != 1 {
		t.Errorf("applied migrations after re-open = %d, want 1", count)
	}
}
