package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
)

// UserStore persists app-local users mapped from authentik OIDC identities.
type UserStore struct {
	db DBTX
}

// NewUserStore returns a user store backed by db.
func NewUserStore(db DBTX) *UserStore {
	return &UserStore{db: db}
}

// UpsertFromClaims creates or refreshes a local user from verified OIDC claims.
func (s *UserStore) UpsertFromClaims(ctx context.Context, claims Claims, adminGroup string) (User, error) {
	role := RoleUser
	if contains(claims.Groups, adminGroup) {
		role = RoleAdmin
	}
	username := claims.Username
	if username == "" {
		username = claims.Email
	}
	if username == "" {
		username = claims.Subject
	}

	existing, ok, err := s.findBySubject(ctx, claims.Subject)
	if err != nil {
		return User{}, err
	}
	if !ok && claims.Email != "" && !claims.EmailUnverified {
		existing, ok, err = s.adoptByEmail(ctx, claims.Email, claims.Subject)
		if err != nil {
			return User{}, err
		}
	}
	if ok {
		_, err = s.db.ExecContext(ctx, `
UPDATE users
SET username = ?, email = ?, display_name = ?, role = ?, updated_at = datetime('now'), last_seen_at = datetime('now')
WHERE oidc_subject = ?`,
			username, claims.Email, claims.Name, role, claims.Subject,
		)
		if err != nil {
			return User{}, fmt.Errorf("update user: %w", err)
		}
		existing.Username = username
		existing.Email = claims.Email
		existing.DisplayName = claims.Name
		existing.Role = role
		return existing, nil
	}

	user := User{
		ID:               newID(),
		OIDCSubject:      claims.Subject,
		Username:         username,
		Email:            claims.Email,
		DisplayName:      claims.Name,
		Role:             role,
		ResponseLanguage: "",
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO users (id, oidc_subject, username, email, display_name, role, response_language, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		user.ID, user.OIDCSubject, user.Username, user.Email, user.DisplayName, user.Role, user.ResponseLanguage,
	)
	if err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	return user, nil
}

// UpdateResponseLanguage persists a user's answer/UI language preference.
func (s *UserStore) UpdateResponseLanguage(ctx context.Context, id, language string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE users
SET response_language = ?, updated_at = datetime('now')
WHERE id = ?`,
		language, id,
	)
	if err != nil {
		return fmt.Errorf("update response language: %w", err)
	}
	return nil
}

// ListUsers returns all app-local users ordered for the admin user list.
func (s *UserStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, oidc_subject, username, email, display_name, role, response_language
FROM users
ORDER BY username ASC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.OIDCSubject, &user.Username, &user.Email, &user.DisplayName, &user.Role, &user.ResponseLanguage); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return users, nil
}

// FindByID returns a user by app-local ID.
func (s *UserStore) FindByID(ctx context.Context, id string) (User, bool, error) {
	var user User
	err := s.db.QueryRowContext(ctx, `
SELECT id, oidc_subject, username, email, display_name, role, response_language
FROM users
WHERE id = ?`,
		id,
	).Scan(&user.ID, &user.OIDCSubject, &user.Username, &user.Email, &user.DisplayName, &user.Role, &user.ResponseLanguage)
	if err == nil {
		return user, true, nil
	}
	if err == sql.ErrNoRows {
		return User{}, false, nil
	}
	return User{}, false, fmt.Errorf("find user by id: %w", err)
}

func (s *UserStore) findBySubject(ctx context.Context, subject string) (User, bool, error) {
	var user User
	err := s.db.QueryRowContext(ctx, `
SELECT id, oidc_subject, username, email, display_name, role, response_language
FROM users
WHERE oidc_subject = ?`,
		subject,
	).Scan(&user.ID, &user.OIDCSubject, &user.Username, &user.Email, &user.DisplayName, &user.Role, &user.ResponseLanguage)
	if err == nil {
		return user, true, nil
	}
	if err == sql.ErrNoRows {
		return User{}, false, nil
	}
	return User{}, false, fmt.Errorf("find user: %w", err)
}

// adoptByEmail re-points an existing account at a new OIDC subject.
//
// Users are keyed by oidc_subject alone, but a subject is only stable within one
// identity provider: swapping providers hands every user a brand-new subject, the
// lookup above misses, and each account is silently replaced by an empty one on
// the owner's first login after the swap (which, thanks to the 30-day session
// cookie, can be weeks later). Matching on the email lets the account survive.
//
// Only an unambiguous match is adopted - exactly one row, non-empty email. Zero
// or several matches fall through to a fresh user, as before.
func (s *UserStore) adoptByEmail(ctx context.Context, email, subject string) (User, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, oidc_subject, username, email, display_name, role, response_language
FROM users
WHERE email <> '' AND lower(email) = lower(?)
LIMIT 2`,
		email,
	)
	if err != nil {
		return User{}, false, fmt.Errorf("find user by email: %w", err)
	}
	defer rows.Close()

	var matches []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.OIDCSubject, &user.Username, &user.Email, &user.DisplayName, &user.Role, &user.ResponseLanguage); err != nil {
			return User{}, false, fmt.Errorf("scan user by email: %w", err)
		}
		matches = append(matches, user)
	}
	if err := rows.Err(); err != nil {
		return User{}, false, fmt.Errorf("iterate users by email: %w", err)
	}
	if len(matches) != 1 {
		return User{}, false, nil
	}

	// Guarding on the subject we selected keeps the claim atomic: a concurrent
	// login that adopted the same row first leaves this update matching nothing,
	// and the caller falls through to creating a user rather than handing back a
	// row that no longer belongs to this subject.
	user := matches[0]
	result, err := s.db.ExecContext(ctx, `
UPDATE users
SET oidc_subject = ?, updated_at = datetime('now')
WHERE id = ? AND oidc_subject = ?`,
		subject, user.ID, user.OIDCSubject,
	)
	if err != nil {
		return User{}, false, fmt.Errorf("adopt user by email: %w", err)
	}
	adopted, err := result.RowsAffected()
	if err != nil {
		return User{}, false, fmt.Errorf("count adopted user: %w", err)
	}
	if adopted == 0 {
		return User{}, false, nil
	}
	user.OIDCSubject = subject
	return user, true, nil
}

func contains(values []string, needle string) bool {
	if needle == "" {
		return false
	}
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
