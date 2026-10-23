package auth

import (
	"errors"
	"net/http"
	"time"

	kitauth "github.com/trick77/ship/kit/auth"
)

// SessionCookieName is the name of the browser cookie carrying the session.
const SessionCookieName = "loom_session"

// minSessionSecret is the shortest BACKEND_SESSION_SECRET accepted. It signs
// every session cookie; a short one can be guessed offline from a single
// cookie, and then any user's session forged.
const minSessionSecret = 32

// NewSessions is kit's signed session cookie, keyed on secret. The cookie
// carries the session itself, so there is no session table: logout clears the
// cookie, and rotating the secret signs everyone out.
func NewSessions(secret string, ttl time.Duration, secure bool) (*kitauth.Sessions, error) {
	if len(secret) < minSessionSecret {
		return nil, errors.New("BACKEND_SESSION_SECRET must be at least 32 characters: it signs the session cookies")
	}
	return kitauth.NewSessions(kitauth.SessionConfig{
		Name:   SessionCookieName,
		Key:    kitauth.DeriveKey(secret, "loom-session"),
		TTL:    ttl,
		Secure: secure,
	})
}

// SessionFor is what a loom session carries: the user's loom id as its
// subject. Role and names are read from the users table on every request, so
// a role change or a deleted user applies to live sessions.
func SessionFor(userID string) kitauth.Claims {
	return kitauth.Claims{Subject: userID}
}

// FromOIDC maps kit's verified OIDC claims onto loom's.
func FromOIDC(c kitauth.Claims) Claims {
	return Claims{
		Subject:       c.Subject,
		Username:      c.PreferredUsername,
		Email:         c.Email,
		Name:          c.Name,
		Groups:        c.Groups,
		EmailVerified: c.EmailVerified,
	}
}

// SessionReader reads a request's session; *kitauth.Sessions is one.
type SessionReader interface {
	Read(*http.Request) (kitauth.Claims, bool)
}
