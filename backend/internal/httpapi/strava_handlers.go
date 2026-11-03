package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/strava"
	kitauth "github.com/trick77/ship/kit/auth"
)

// StravaService is the loom-strava sidecar's connection routes, each for one
// loom user's own Strava account; *strava.Client implements it.
type StravaService interface {
	Status(ctx context.Context, user string, check bool) (strava.Status, error)
	AuthorizeURL(ctx context.Context, user, redirectURI, state string) (string, error)
	Connect(ctx context.Context, user, code, scope string) (strava.Status, error)
	Disconnect(ctx context.Context, user string) (strava.Status, error)
}

const (
	// stravaCallbackPath is where Strava sends the browser back; it must sit
	// on the app's Authorization Callback Domain (PUBLIC_FQDN).
	stravaCallbackPath = "/api/strava/callback"
	// stravaStateCookie holds the OAuth state between connect and callback:
	// the callback proves the answer belongs to this browser's request.
	stravaStateCookie = "loom_strava_state"
	// stravaStateTTL is how long the consent page may take.
	stravaStateTTL = 10 * time.Minute
	// maxStravaCodeBytes bounds the code and scope Strava hands back.
	maxStravaCodeBytes = 512
)

// stravaAllowed reports whether the account may connect Strava: it is on
// BACKEND_STRAVA_USERS. Each listed user connects their own account.
func (s *server) stravaAllowed(user auth.User) bool {
	return featureAllowed(s.strava != nil, s.stravaUsers, user)
}

func (s *server) stravaUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	return listedUser(w, r, s.strava != nil, s.stravaUsers)
}

func (s *server) handleStravaStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := s.stravaUser(w, r)
	if !ok {
		return
	}
	st, err := s.strava.Status(r.Context(), user.ID, false)
	s.writeStrava(w, r, st, err)
}

func (s *server) handleStravaCheck(w http.ResponseWriter, r *http.Request) {
	user, ok := s.stravaUser(w, r)
	if !ok {
		return
	}
	st, err := s.strava.Status(r.Context(), user.ID, true)
	s.writeStrava(w, r, st, err)
}

func (s *server) handleStravaDisconnect(w http.ResponseWriter, r *http.Request) {
	user, ok := s.stravaUser(w, r)
	if !ok {
		return
	}
	st, err := s.strava.Disconnect(r.Context(), user.ID)
	s.writeStrava(w, r, st, err)
}

// handleStravaConnect sends the browser to Strava's consent page. A top-level
// navigation, not a fetch: the page goes to Strava and comes back to the
// callback.
func (s *server) handleStravaConnect(w http.ResponseWriter, r *http.Request) {
	user, ok := s.stravaUser(w, r)
	if !ok {
		return
	}
	state := base64.RawURLEncoding.EncodeToString(kitauth.RandomKey())
	target, err := s.strava.AuthorizeURL(r.Context(), user.ID, s.stravaRedirectURI(), state)
	if err != nil {
		slog.ErrorContext(r.Context(), "strava authorize URL failed", "err", err)
		s.backToSettings(w, r, "unavailable")
		return
	}
	// The cookie names the user who started: Strava's code is that user's
	// alone, whoever is signed in when the browser comes back.
	http.SetCookie(w, s.stravaState(state+"."+user.ID, time.Now().Add(stravaStateTTL)))
	http.Redirect(w, r, target, http.StatusFound)
}

// stravaStateFor reports whether the state cookie belongs to user and
// carries the state Strava sent back.
func stravaStateFor(r *http.Request, user auth.User, state string) bool {
	sc, err := r.Cookie(stravaStateCookie)
	if err != nil {
		return false
	}
	nonce, owner, ok := strings.Cut(sc.Value, ".")
	return ok && nonce != "" && owner == user.ID &&
		subtle.ConstantTimeCompare([]byte(nonce), []byte(state)) == 1
}

// handleStravaCallbackSignedOut answers a callback whose loom session ended
// during the consent: back to the app, which says to sign in and connect
// again. The code is dropped: it cannot be tied to a user.
func (s *server) handleStravaCallbackSignedOut(w http.ResponseWriter, r *http.Request) {
	s.backToSettings(w, r, "signin")
}

// handleStravaCallback finishes the OAuth: the state must match this
// browser's cookie and the signed-in user must be listed; the code goes to
// the sidecar as that user's. Only an answer carrying the right state counts,
// refusals included, and only that one clears the cookie (single-use): a
// forged callback another page sends the browser to must neither end the
// real one nor show its outcome.
func (s *server) handleStravaCallback(w http.ResponseWriter, r *http.Request) {
	user, ok := s.stravaUser(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	if !stravaStateFor(r, user, q.Get("state")) {
		s.backToSettings(w, r, "state")
		return
	}
	http.SetCookie(w, s.stravaState("", time.Unix(0, 0)))
	if q.Get("error") != "" {
		// The user said no on Strava's page: nothing to debug.
		s.backToSettings(w, r, "denied")
		return
	}
	code, scope := q.Get("code"), q.Get("scope")
	if code == "" || len(code) > maxStravaCodeBytes || len(scope) > maxStravaCodeBytes {
		s.backToSettings(w, r, "state")
		return
	}
	_, err := s.strava.Connect(r.Context(), user.ID, code, scope)
	var e *strava.Error
	switch {
	case err == nil:
		s.backToSettings(w, r, "connected")
	case errors.As(err, &e):
		outcome := e.Code
		switch e.Code {
		case strava.CodeRejected:
			// Strava's own refusal (a used or expired code): nothing to debug.
		case strava.CodeMalformed:
			// A code the sidecar could not use: a bug somewhere, logged; the
			// user starts again like after a refusal.
			slog.ErrorContext(r.Context(), "strava connect failed", "err", err)
			outcome = strava.CodeRejected
		default:
			slog.ErrorContext(r.Context(), "strava connect failed", "err", err)
		}
		s.backToSettings(w, r, outcome)
	default:
		slog.ErrorContext(r.Context(), "strava connect failed", "err", err)
		s.backToSettings(w, r, "unavailable")
	}
}

// backToSettings returns the browser to the app with Settings → Strava open
// and the outcome to show (the page words it by code).
func (s *server) backToSettings(w http.ResponseWriter, r *http.Request, outcome string) {
	http.Redirect(w, r, "/?strava="+url.QueryEscape(outcome), http.StatusFound)
}

// stravaRedirectURI is the callback on loom's public URL: the one Strava's app
// settings name as the Authorization Callback Domain.
func (s *server) stravaRedirectURI() string {
	return strings.TrimRight(s.publicURL, "/") + stravaCallbackPath
}

// stravaState is the state cookie, scoped to the callback, Secure on https,
// SameSite Lax: the callback is a top-level navigation from Strava, which a
// Strict cookie would be withheld from.
func (s *server) stravaState(value string, exp time.Time) *http.Cookie {
	c := &http.Cookie{ //nolint:gosec // HttpOnly and SameSite are set; Secure follows the public URL so local http works
		Name: stravaStateCookie, Value: value, Path: stravaCallbackPath, Expires: exp,
		HttpOnly: true, Secure: strings.HasPrefix(s.publicURL, "https://"), SameSite: http.SameSiteLaxMode,
	}
	if value == "" {
		c.MaxAge = -1
	}
	return c
}

// stravaStatus maps a sidecar code to the response status: a refused or
// malformed code 422, Strava 502, the sidecar's storage 500, the rest 503.
var stravaStatus = map[string]int{
	strava.CodeRejected:  http.StatusUnprocessableEntity,
	strava.CodeMalformed: http.StatusUnprocessableEntity,
	strava.CodeUpstream:  http.StatusBadGateway,
	strava.CodeStorage:   http.StatusInternalServerError,
	strava.CodeConfig:    http.StatusServiceUnavailable,
}

func (s *server) writeStrava(w http.ResponseWriter, r *http.Request, st strava.Status, err error) {
	writeSidecar(w, r, st, err, stravaStatus, "Strava connection unavailable")
}
