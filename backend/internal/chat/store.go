package chat

import (
	"context"
	"database/sql"
)

// DBTX is the subset of *sql.DB used by chat stores.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	db DBTX
}

func NewStore(db DBTX) *Store {
	return &Store{db: db}
}
