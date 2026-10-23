// Package store opens the SQLite database (pure-Go ncruces driver with
// sqlite-vec linked in) and applies embedded migrations.
package store

import (
	"database/sql"
	"fmt"

	"github.com/trick77/ship/kit/sqlitex"

	// sqlite-vec WASM build for ncruces; provides the SQLite WASM binary AND
	// the vec0 virtual table + vec_* functions. Replaces ncruces/go-sqlite3/embed.
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	// registers the "sqlite3" database/sql driver.
	_ "github.com/ncruces/go-sqlite3/driver"
)

// Open opens (creating if needed) the SQLite database at path, applies PRAGMAs
// for safe concurrent use, runs migrations, and returns the *sql.DB.
func Open(path string) (*sql.DB, error) {
	// Migrations run on a pool of their own, closed before the app's opens: a
	// table rebuild switches foreign keys off on its connection, and one that
	// fails midway must not hand that connection to the app.
	mdb, err := sql.Open("sqlite3", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	err = migrate(mdb)
	_ = mdb.Close()
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	db, err := sql.Open("sqlite3", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return db, nil
}

// dsn builds the connection string. WAL for concurrent readers, a busy timeout
// so writers queue, foreign keys enforced on every connection, and
// _txlock=immediate so every BeginTx takes the write lock up front: every
// transaction in this codebase writes, and a deferred transaction that has
// already read cannot wait for the lock (SQLite returns busy at once to avoid
// a deadlock), so two concurrent write transactions would fail instead of
// queueing.
func dsn(path string) string {
	return sqlitex.DSN(path, "_txlock=immediate")
}
