package auth

import (
	"context"
	"net/http"

	"github.com/trick77/ship/kit/httpx"
)

// UserLookup is the user lookup dependency required by Middleware.
type UserLookup interface {
	FindByID(context.Context, string) (User, bool, error)
}

// Middleware protects authenticated and admin-only routes.
type Middleware struct {
	sessions SessionReader
	users    UserLookup
}

// NewMiddleware returns auth middleware.
func NewMiddleware(sessions SessionReader, users UserLookup) *Middleware {
	return &Middleware{sessions: sessions, users: users}
}

// RequireAuth rejects requests without a valid Loom session, or whose user no
// longer exists.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := m.sessions.Read(r)
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, ok, err := m.users.FindByID(r.Context(), session.Subject)
		if err != nil {
			httpx.ServerError(w, r, err, "user lookup failed")
			return
		}
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin rejects authenticated non-admin users.
func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role != RoleAdmin {
			httpx.WriteError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}
