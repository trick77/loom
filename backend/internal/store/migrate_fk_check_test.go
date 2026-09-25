package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// openWithOrphan returns a migrated-from-scratch database holding one child
// row whose parent does not exist, the kind of legacy row an older bug leaves
// behind. Foreign keys are on for every connection, so the orphan is inserted
// with enforcement off on a dedicated connection.
func openWithOrphan(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn(filepath.Join(t.TempDir(), "orphan.db")))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE parents (id INTEGER PRIMARY KEY);
CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parents(id))`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	for _, stmt := range []string{
		`PRAGMA foreign_keys = OFF`,
		`INSERT INTO children (id, parent_id) VALUES (1, 99)`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := conn.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return db
}

// An ordinary migration must not be blocked by an orphan row it did not
// create: the database-wide check would turn one legacy row into a boot
// failure at the next release.
func TestApplyInTransactionIgnoresPreExistingOrphans(t *testing.T) {
	db := openWithOrphan(t)
	if err := applyInTransaction(db, "9001_index.sql", []byte(`CREATE INDEX idx_children_parent ON children(parent_id)`)); err != nil {
		t.Fatalf("applyInTransaction() error = %v, want nil", err)
	}
}

// A migration that ran with enforcement off is the one place the check
// belongs: a table rebuild that leaves a dangling reference must roll back.
func TestApplyWithForeignKeysOffRejectsOrphans(t *testing.T) {
	db := openWithOrphan(t)
	err := applyWithForeignKeysOff(db, "9002_rebuild.sql", []byte(`CREATE INDEX idx_children_parent ON children(parent_id)`))
	if err == nil || !strings.Contains(err.Error(), "foreign key check failed") {
		t.Fatalf("applyWithForeignKeysOff() error = %v, want a foreign key check failure", err)
	}
	var dummy int
	if err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, "9002_rebuild.sql").Scan(&dummy); err != sql.ErrNoRows {
		t.Fatalf("migration was recorded despite the failed check (err = %v)", err)
	}
}
