package store

import (
	"path/filepath"
	"testing"
)

// A database migrated by loom's own runner keys schema_migrations on version
// and records the names before the renumbering. Adopted, it must apply
// nothing again (a re-run would fail on a table that exists) and keep the
// rows that are no migration (the rag package's one-time markers).
func TestMigrate_adoptsLegacyBookkeeping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Back to what the old runner left behind.
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	exec(`ALTER TABLE schema_migrations RENAME COLUMN name TO version`)
	for old, name := range renamedMigrations {
		exec(`UPDATE schema_migrations SET version = ? WHERE version = ?`, old, name)
	}
	exec(`INSERT INTO schema_migrations (version) VALUES ('some_marker_v1')`)
	var before int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	db, err = Open(path)
	if err != nil {
		t.Fatalf("Open after adoption: %v", err)
	}
	defer func() { _ = db.Close() }()
	var after int
	if err := db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("schema_migrations rows = %d after adoption, want %d", after, before)
	}
	for _, name := range []string{"0040_queued_messages.sql", "0006_user_usage.sql", "0005_documents_rag.sql", "some_marker_v1"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE name = ?`, name).Scan(&n); err != nil || n != 1 {
			t.Errorf("row %s = %d (err %v), want 1", name, n, err)
		}
	}
	// Adopting twice is a no-op.
	if err := adoptLegacyBookkeeping(t.Context(), db); err != nil {
		t.Fatalf("second adoption: %v", err)
	}
}

// Every renumbered file exists, and no new name is another file's old one:
// the adoption's updates rely on it.
func TestRenamedMigrationsAreConsistent(t *testing.T) {
	var err error
	for old, name := range renamedMigrations {
		if _, err = migrationsFS.ReadFile("migrations/" + name); err != nil {
			t.Errorf("%s -> %s: %v", old, name, err)
		}
		if other, ok := renamedMigrations[name]; ok {
			t.Errorf("%s is the new name of %s and the old name of a file renamed to %s", name, old, other)
		}
	}
}
