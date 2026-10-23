package store

import (
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"
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
	if err := migrateFS(db, migrationsUpTo(t, last)); err != nil {
		t.Fatalf("migrate up to %s: %v", last, err)
	}
	return db
}

// migrationsUpTo is the embedded migrations through last; the names are
// zero-padded, so name order is version order.
func migrationsUpTo(t *testing.T, last string) fs.FS {
	t.Helper()
	entries, err := fs.ReadDir(migrations(), ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	out := fstest.MapFS{}
	for _, e := range entries {
		if e.Name() > last {
			continue
		}
		body, err := fs.ReadFile(migrations(), e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		out[e.Name()] = &fstest.MapFile{Data: body}
	}
	return out
}
