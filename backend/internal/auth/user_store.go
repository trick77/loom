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
		ResponseLanguage: "auto",
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
