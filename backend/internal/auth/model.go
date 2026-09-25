package auth

import (
	"context"
	"database/sql"
)

// Role is the app-local authorization role mapped from OIDC groups.
type Role string

// The roles a user can hold. RoleAdmin is granted through the admin group in
// the OIDC claims; everyone else authenticates as RoleUser.
const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// DevAdminGroup is the synthetic group used only by local development auth.
const DevAdminGroup = "loom-dev-admin"

// User is Loom's app-local user profile.
type User struct {
	ID               string `json:"id"`
	OIDCSubject      string `json:"-"`
	Username         string `json:"username"`
	Email            string `json:"email"`
	DisplayName      string `json:"displayName"`
	Role             Role   `json:"role"`
	ResponseLanguage string `json:"responseLanguage"`
}

// Claims contains the verified OIDC identity fields Loom needs.
type Claims struct {
	Subject  string
	Username string
	Email    string
	Name     string
	Groups   []string
	// EmailVerified is set only when the provider explicitly vouches for the
	// email (email_verified=true). Only a verified email may identify an
	// existing account for adoption; an absent claim is "not stated".
	EmailVerified bool
}

type contextKey string

const userContextKey contextKey = "loom_user"

// UserFromContext returns the authenticated user stored on a request context.
func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey).(User)
	return user, ok
}

// DBTX is the subset of *sql.DB used by auth stores.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
