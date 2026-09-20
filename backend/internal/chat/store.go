package chat

import (
	"context"
	"database/sql"
)

// DBTX is the subset of *sql.DB used by chat stores.
type DBTX interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Store encapsulates database operations for chat data (projects, threads, messages).
type Store struct {
	db DBTX
}

// NewStore creates a new Store backed by the provided database connection.
func NewStore(db DBTX) *Store {
	return &Store{db: db}
}
