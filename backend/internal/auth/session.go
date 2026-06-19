package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"
)

const SessionCookieName = "loom_session"

// Session is the server-side representation of an authenticated browser session.
type Session struct {
	Token     string
	UserID    string
	ExpiresAt time.Time
}

// SessionStore persists opaque browser sessions.
type SessionStore struct {
	db     DBTX
	secure bool
}

// NewSessionStore returns a SQLite-backed session store.
func NewSessionStore(db DBTX, secure bool) *SessionStore {
	return &SessionStore{db: db, secure: secure}
}

// Create stores a hashed session token and returns the raw token for the cookie.
func (s *SessionStore) Create(ctx context.Context, userID string, ttl time.Duration) (Session, error) {
	token := randomToken()
	expiresAt := time.Now().UTC().Add(ttl)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (token_hash, user_id, expires_at)
VALUES (?, ?, ?)`,
		hashToken(token), userID, formatTime(expiresAt),
	)
	if err != nil {
		return Session{}, fmt.Errorf("insert session: %w", err)
	}
	return Session{Token: token, UserID: userID, ExpiresAt: expiresAt}, nil
}

// Lookup returns the active session for token.
func (s *SessionStore) Lookup(ctx context.Context, token string) (Session, bool, error) {
	var session Session
	var expires string
	err := s.db.QueryRowContext(ctx, `
SELECT user_id, expires_at
FROM sessions
WHERE token_hash = ? AND expires_at > datetime('now')`,
		hashToken(token),
	).Scan(&session.UserID, &expires)
	if err == sql.ErrNoRows {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("lookup session: %w", err)
	}
	expiresAt, err := parseDBTime(expires)
	if err != nil {
		return Session{}, false, err
	}
	session.Token = token
	session.ExpiresAt = expiresAt
	_, _ = s.db.ExecContext(ctx, `
UPDATE sessions
SET last_seen_at = datetime('now')
WHERE token_hash = ? AND last_seen_at < datetime('now', '-1 minute')`,
		hashToken(token),
	)
	return session, true, nil
}

// DeleteExpired removes expired sessions and returns how many rows were deleted.
func (s *SessionStore) DeleteExpired(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= datetime('now')`)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted sessions: %w", err)
	}
	return deleted, nil
}

// Revoke deletes the session for token.
func (s *SessionStore) Revoke(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// CookieFor builds the browser session cookie.
func (s *SessionStore) CookieFor(token string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearCookie returns a cookie that clears the browser session.
func (s *SessionStore) ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func formatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

func parseDBTime(value string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse db time: %w", err)
	}
	return t, nil
}
