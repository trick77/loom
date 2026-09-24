// Package store opens the SQLite database (pure-Go ncruces driver with
// sqlite-vec linked in) and applies embedded migrations.
package store

import (
	"database/sql"
	"fmt"
	"net/url"

	// sqlite-vec WASM build for ncruces; provides the SQLite WASM binary AND
	// the vec0 virtual table + vec_* functions. Replaces ncruces/go-sqlite3/embed.
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	// registers the "sqlite3" database/sql driver.
	_ "github.com/ncruces/go-sqlite3/driver"
)

// Open opens (creating if needed) the SQLite database at path, applies PRAGMAs
// for safe concurrent use, runs migrations, and returns the *sql.DB.
func Open(path string) (*sql.DB, error) {
	// _txlock=immediate: every BeginTx takes the write lock up front. Every
	// transaction in this codebase writes, and a deferred transaction that has
	// already read cannot wait for the lock (SQLite returns busy at once to
	// avoid a deadlock), so two concurrent write transactions would fail
	// instead of queueing on busy_timeout.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(wal)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(on)&_txlock=immediate",
		url.PathEscape(path),
	)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
