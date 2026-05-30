package auth

import (
	"context"
	"database/sql"
)

// Role is the app-local authorization role mapped from authentik groups.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// User is Spark's app-local user profile.
type User struct {
	ID               string `json:"id"`
	OIDCSubject      string `json:"-"`
	Username         string `json:"username"`
	Email            string `json:"email"`
	DisplayName      string `json:"displayName"`
	Role             Role   `json:"role"`
	ResponseLanguage string `json:"responseLanguage"`
}

// Claims contains the verified OIDC identity fields Spark needs.
type Claims struct {
	Subject  string
	Username string
	Email    string
	Name     string
	Groups   []string
}

type contextKey string

const userContextKey contextKey = "spark_user"

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
