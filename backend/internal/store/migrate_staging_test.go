package store

import (
	"database/sql"
	"testing"
)

// openMigratedUpTo opens path and applies migrations through last (a filename),
// so a test can seed rows in an older schema and then run the migrations that
// follow against real data.
func openMigratedUpTo(t *testing.T, path, last string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn(path))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrateUpTo(db, last); err != nil {
		t.Fatalf("migrate up to %s: %v", last, err)
	}
	return db
}
