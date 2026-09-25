package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// foreignKeysOffDirective, as the first line of a migration file, runs that
// migration with foreign key enforcement off, the way SQLite's documented
// table-rebuild procedure requires: with enforcement on, DROP TABLE performs
// an implicit DELETE that fires ON DELETE CASCADE on every child table. The
// migration runs on a single dedicated connection (the pragma is per
// connection and a no-op inside a transaction), and PRAGMA foreign_key_check
// must come back clean before it commits.
const foreignKeysOffDirective = "-- loom:foreign_keys=off"

// migrate applies any embedded migrations not yet recorded in schema_migrations,
// each in its own transaction, in lexicographic filename order.
func migrate(db *sql.DB) error {
	return migrateUpTo(db, "")
}

// migrateUpTo is migrate that stops after the migration named last (when
// non-empty); tests use it to stage a database at an older schema version.
func migrateUpTo(db *sql.DB, last string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if last != "" && name > last {
			break
		}
		var dummy int
		err := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, name).Scan(&dummy)
		if err == nil {
			continue // already applied
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if strings.HasPrefix(string(body), foreignKeysOffDirective) {
			err = applyWithForeignKeysOff(db, name, body)
		} else {
			err = applyInTransaction(db, name, body)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func applyInTransaction(db *sql.DB, name string, body []byte) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	return runMigration(tx, name, body, false)
}

// applyWithForeignKeysOff runs one migration on a dedicated connection with
// foreign keys disabled, verifies the schema with PRAGMA foreign_key_check
// before committing, and re-enables enforcement before the connection returns
// to the pool.
func applyWithForeignKeysOff(db *sql.DB, name string, body []byte) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for %s: %w", name, err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	return runMigration(tx, name, body, true)
}

// runMigration executes body and records name inside tx. With verifyForeignKeys
// it counts PRAGMA foreign_key_check violations before and after, the
// safeguard for a migration that ran with enforcement off, and rolls back only
// when the migration added some: a legacy orphan row that was already there
// must not stop the upgrade. Without it the check is not run at all.
func runMigration(tx *sql.Tx, name string, body []byte, verifyForeignKeys bool) error {
	var violationsBefore int
	if verifyForeignKeys {
		n, err := countForeignKeyViolations(tx)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("foreign key check before %s: %w", name, err)
		}
		violationsBefore = n
	}
	if _, err := tx.Exec(string(body)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record %s: %w", name, err)
	}
	if verifyForeignKeys {
		violationsAfter, err := countForeignKeyViolations(tx)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("foreign key check after %s: %w", name, err)
		}
		if violationsAfter > violationsBefore {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: foreign key check failed (%d new violations)", name, violationsAfter-violationsBefore)
		}
	}
	return tx.Commit()
}

func countForeignKeyViolations(tx *sql.Tx) (int, error) {
	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		n++
	}
	return n, rows.Err()
}
