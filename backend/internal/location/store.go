// Package location keeps where each user is, as derived from their memory:
// a home place and an optional current one with an end date.
package location

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// DBTX is the subset of *sql.DB the store uses.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Store persists users' places. Every query is scoped by user.
type Store struct {
	db DBTX
}

// NewStore returns a store backed by db.
func NewStore(db DBTX) *Store {
	return &Store{db: db}
}

// Places is the user's places and the hash of the facts they were derived
// from; "" when they never were.
func (s *Store) Places(ctx context.Context, userID string) (home, current *Place, factsHash string, err error) {
	if err := s.db.QueryRowContext(ctx, `SELECT facts_hash FROM user_location_state WHERE user_id = ?`, userID).Scan(&factsHash); err != nil && err != sql.ErrNoRows {
		return nil, nil, "", fmt.Errorf("read location state: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT kind, name, region, country, latitude, longitude, timezone, COALESCE(until, ''), COALESCE(fact_id, '')
FROM user_locations WHERE user_id = ?`, userID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("list places: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p Place
		if err := rows.Scan(&p.Kind, &p.Name, &p.Region, &p.Country, &p.Latitude, &p.Longitude, &p.Timezone, &p.Until, &p.FactID); err != nil {
			return nil, nil, "", fmt.Errorf("scan place: %w", err)
		}
		switch p.Kind {
		case KindHome:
			home = &p
		case KindCurrent:
			current = &p
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, "", fmt.Errorf("iterate places: %w", err)
	}
	return home, current, factsHash, nil
}

// Save replaces the user's places with home and current (either may be nil)
// and records the facts they came from.
func (s *Store) Save(ctx context.Context, userID, factsHash string, home, current *Place) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save places: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_locations WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("save places: clear: %w", err)
	}
	for kind, p := range map[string]*Place{KindHome: home, KindCurrent: current} {
		if p == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO user_locations (user_id, kind, name, region, country, latitude, longitude, timezone, until, fact_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))`,
			userID, kind, p.Name, p.Region, p.Country, p.Latitude, p.Longitude, p.Timezone, p.Until, p.FactID); err != nil {
			return fmt.Errorf("save places: %s: %w", kind, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO user_location_state (user_id, facts_hash) VALUES (?, ?)
ON CONFLICT (user_id) DO UPDATE SET facts_hash = excluded.facts_hash, resolved_at = datetime('now')`,
		userID, factsHash); err != nil {
		return fmt.Errorf("save places: state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save places: commit: %w", err)
	}
	return nil
}

// Here is the place that holds on today (YYYY-MM-DD in the user's zone): the
// current one until its last day, then home; nil when neither is known.
func Here(home, current *Place, today string) *Place {
	if current != nil && (current.Until == "" || current.Until >= today) {
		return current
	}
	return home
}

// FactsHash fingerprints the facts a place is read from: their ids, text and
// the day each was last seen (a stay "this week" ends by that day).
func FactsHash(facts []chat.UserFact) string {
	h := sha256.New()
	for _, f := range facts {
		// A hash never fails a write.
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00", f.ID, f.Content, f.LastSeen.UTC().Format(time.DateOnly))
	}
	return hex.EncodeToString(h.Sum(nil))
}
