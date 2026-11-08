package webpush

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/trick77/loom/internal/sqlutil"
)

// MaxSubscriptions bounds a user's devices: every notification goes to each.
const MaxSubscriptions = 10

// Stored is a saved subscription.
type Stored struct {
	Subscription
	ID            string
	UserAgent     string
	LastSuccessAt *time.Time
}

// Store keeps the browsers' subscriptions.
type Store struct{ db *sql.DB }

// NewStore keeps the subscriptions in db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Save adds or renews sub for userID and drops the user's oldest past
// MaxSubscriptions.
func (st *Store) Save(ctx context.Context, userID string, sub Subscription, userAgent string) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save push subscription: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(endpoint) DO UPDATE SET
    user_id = excluded.user_id, p256dh = excluded.p256dh, auth = excluded.auth,
    user_agent = excluded.user_agent, updated_at = datetime('now'),
    last_success_at = CASE WHEN push_subscriptions.user_id = excluded.user_id THEN push_subscriptions.last_success_at END`,
		sqlutil.NewID(), userID, sub.Endpoint, sub.P256dh, sub.Auth, userAgent); err != nil {
		return fmt.Errorf("save push subscription: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
DELETE FROM push_subscriptions WHERE user_id = ? AND id NOT IN (
    SELECT id FROM push_subscriptions WHERE user_id = ? ORDER BY updated_at DESC, rowid DESC LIMIT ?)`,
		userID, userID, MaxSubscriptions); err != nil {
		return fmt.Errorf("trim push subscriptions: %w", err)
	}
	return tx.Commit()
}

// Delete removes the user's subscription at endpoint.
func (st *Store) Delete(ctx context.Context, userID, endpoint string) (bool, error) {
	res, err := st.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ?`, userID, endpoint)
	if err != nil {
		return false, fmt.Errorf("delete push subscription: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// List returns the user's subscriptions, most recently saved first.
func (st *Store) List(ctx context.Context, userID string) ([]Stored, error) {
	rows, err := st.db.QueryContext(ctx, `
SELECT id, endpoint, p256dh, auth, user_agent, last_success_at FROM push_subscriptions
WHERE user_id = ? ORDER BY updated_at DESC, rowid DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Stored{}
	for rows.Next() {
		var s Stored
		var last sql.NullString
		if err := rows.Scan(&s.ID, &s.Endpoint, &s.P256dh, &s.Auth, &s.UserAgent, &last); err != nil {
			return nil, fmt.Errorf("scan push subscription: %w", err)
		}
		if last.Valid {
			t, err := sqlutil.ParseTime(last.String)
			if err != nil {
				return nil, err
			}
			s.LastSuccessAt = &t
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MarkSent notes a delivery the push service accepted.
func (st *Store) MarkSent(ctx context.Context, userID, endpoint string, at time.Time) error {
	if _, err := st.db.ExecContext(ctx, `UPDATE push_subscriptions SET last_success_at = ? WHERE user_id = ? AND endpoint = ?`,
		at.UTC().Format(time.DateTime), userID, endpoint); err != nil {
		return fmt.Errorf("mark push sent: %w", err)
	}
	return nil
}
