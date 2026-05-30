// Package httpapi builds spark's HTTP handler: JSON/SSE API plus the embedded SPA.
package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/trick77/spark/internal/auth"
)

// Deps are the dependencies needed to build the server. Grows in later phases
// (store, config, services); for Phase 1 only Version and the static handler.
type Deps struct {
	Version string
	Static  http.Handler // serves the embedded SPA; may be nil in tests

	OIDC                  OIDCService
	Auth                  *auth.Middleware
	Sessions              SessionService
	Users                 UserService
	OIDCAdminGroup        string
	PostLogoutRedirectURL string
}

type server struct {
	version               string
	oidc                  OIDCService
	auth                  *auth.Middleware
	sessions              SessionService
	users                 UserService
	oidcAdminGroup        string
	postLogoutRedirectURL string
}

// OIDCService is the auth handler dependency for OIDC redirects and callbacks.
type OIDCService interface {
	StartLogin(http.ResponseWriter, *http.Request)
	HandleCallback(*http.Request) (auth.Claims, error)
}

// SessionService is the session dependency used by auth handlers.
type SessionService interface {
	Create(context.Context, string, time.Duration) (auth.Session, error)
	Lookup(context.Context, string) (auth.Session, bool, error)
	Revoke(context.Context, string) error
	CookieFor(string, time.Time) *http.Cookie
	ClearCookie() *http.Cookie
}

// UserService is the user dependency used by auth handlers.
type UserService interface {
	auth.UserLookup
	UpsertFromClaims(context.Context, auth.Claims, string) (auth.User, error)
	ListUsers(context.Context) ([]auth.User, error)
}

// New returns the fully wired HTTP handler.
func New(d Deps) http.Handler {
	s := &server{
		version:               d.Version,
		oidc:                  d.OIDC,
		auth:                  d.Auth,
		sessions:              d.Sessions,
		users:                 d.Users,
		oidcAdminGroup:        d.OIDCAdminGroup,
		postLogoutRedirectURL: d.PostLogoutRedirectURL,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/health/stream", s.handleHealthStream)
	mux.HandleFunc("GET /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("GET /api/auth/callback", s.handleAuthCallback)
	mux.Handle("POST /api/auth/logout", s.requireAuth(http.HandlerFunc(s.handleAuthLogout)))
	mux.Handle("GET /api/me", s.requireAuth(http.HandlerFunc(s.handleMe)))
	mux.Handle("GET /api/admin/users", s.requireAuth(s.requireAdmin(http.HandlerFunc(s.handleAdminUsers))))
	if d.Static != nil {
		mux.Handle("/", d.Static)
	}

	return logging(recovery(mux))
}

func (s *server) requireAuth(next http.Handler) http.Handler {
	if s.auth == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		})
	}
	return s.auth.RequireAuth(next)
}

func (s *server) requireAdmin(next http.Handler) http.Handler {
	if s.auth == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		})
	}
	return s.auth.RequireAdmin(next)
}
