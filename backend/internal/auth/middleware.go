package auth

import (
	"context"
	"encoding/json"
	"net/http"
)

// SessionLookup is the session lookup dependency required by Middleware.
type SessionLookup interface {
	Lookup(context.Context, string) (Session, bool, error)
}

// UserLookup is the user lookup dependency required by Middleware.
type UserLookup interface {
	FindByID(context.Context, string) (User, bool, error)
}

// Middleware protects authenticated and admin-only routes.
type Middleware struct {
	sessions SessionLookup
	users    UserLookup
}

// NewMiddleware returns auth middleware.
func NewMiddleware(sessions SessionLookup, users UserLookup) *Middleware {
	return &Middleware{sessions: sessions, users: users}
}

// RequireAuth rejects requests without a valid Slopr session.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		session, ok, err := m.sessions.Lookup(r.Context(), cookie.Value)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, ok, err := m.users.FindByID(r.Context(), session.UserID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "user lookup failed")
			return
		}
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
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
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if user.Role != RoleAdmin {
			writeJSONError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
