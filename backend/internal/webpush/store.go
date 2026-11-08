package webpush

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/trick77/loom/internal/sqlutil"
)

// MaxSubscriptions bounds a user's devices: every notification goes to each.
const MaxSubscriptions = 10

// ErrTaken is an endpoint another user holds, offered with other keys: only
// the browser that owns the subscription (it knows the auth secret) moves it.
var ErrTaken = errors.New("push endpoint belongs to another user")

// Stored is a saved subscription.
type Stored struct {
	Subscription
	ID            string
	UserAgent     string
	VAPIDKey      string
	LastSuccessAt *time.Time
}

// Store keeps the browsers' subscriptions.
type Store struct{ db *sql.DB }

// NewStore keeps the subscriptions in db.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Save adds or renews sub for userID, made with vapidKey, and drops the
// user's oldest past MaxSubscriptions. The same browser signed in as someone
// else re-saves its own keys and so moves the row; anyone else gets ErrTaken.
func (st *Store) Save(ctx context.Context, userID string, sub Subscription, userAgent, vapidKey string) error {
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save push subscription: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent, vapid_key)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(endpoint) DO UPDATE SET
    user_id = excluded.user_id, p256dh = excluded.p256dh, auth = excluded.auth,
    user_agent = excluded.user_agent, vapid_key = excluded.vapid_key, updated_at = datetime('now'),
    last_success_at = CASE WHEN push_subscriptions.user_id = excluded.user_id THEN push_subscriptions.last_success_at END
WHERE push_subscriptions.user_id = excluded.user_id
   OR (push_subscriptions.p256dh = excluded.p256dh AND push_subscriptions.auth = excluded.auth)`,
		sqlutil.NewID(), userID, sub.Endpoint, sub.P256dh, sub.Auth, userAgent, vapidKey)
	if err != nil {
		return fmt.Errorf("save push subscription: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("save push subscription: %w", err)
	} else if n == 0 {
		return ErrTaken
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

// DeleteGone removes the user's subscription only while it still holds sub's
// keys: a browser that renewed it meanwhile (same endpoint, new keys) keeps
// the renewed row.
func (st *Store) DeleteGone(ctx context.Context, userID string, sub Subscription) error {
	if _, err := st.db.ExecContext(ctx, `
DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ? AND p256dh = ? AND auth = ?`,
		userID, sub.Endpoint, sub.P256dh, sub.Auth); err != nil {
		return fmt.Errorf("delete gone push subscription: %w", err)
	}
	return nil
}

// List returns the user's subscriptions, most recently saved first.
func (st *Store) List(ctx context.Context, userID string) ([]Stored, error) {
	rows, err := st.db.QueryContext(ctx, `
SELECT id, endpoint, p256dh, auth, user_agent, vapid_key, last_success_at FROM push_subscriptions
WHERE user_id = ? ORDER BY updated_at DESC, rowid DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Stored{}
	for rows.Next() {
		var s Stored
		var last sql.NullString
		if err := rows.Scan(&s.ID, &s.Endpoint, &s.P256dh, &s.Auth, &s.UserAgent, &s.VAPIDKey, &last); err != nil {
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
