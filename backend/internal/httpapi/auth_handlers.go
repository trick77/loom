package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/ship/kit/httpx"
)

func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.devAuthClaims.Subject != "" {
		s.createSessionFromClaims(w, r, s.devAuthClaims, auth.DevAdminGroup)
		return
	}
	if s.oidc == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "oidc is not configured")
		return
	}
	s.oidc.Start(w, r)
}

func (s *server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "oidc is not configured")
		return
	}
	claims, err := s.oidc.Callback(w, r)
	if err != nil {
		// Warn, not Error: a user abandoning the consent screen reaches this
		// path too, so it is not necessarily a server fault.
		slog.Warn("oidc callback failed", "err", redactErr(err))
		http.Redirect(w, r, "/?auth_error=oidc_callback_failed", http.StatusFound)
		return
	}
	s.createSessionFromClaims(w, r, auth.FromOIDC(claims), s.oidcAdminGroup)
}

func (s *server) createSessionFromClaims(w http.ResponseWriter, r *http.Request, claims auth.Claims, adminGroup string) {
	if s.users == nil || s.sessions == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "auth is not configured")
		return
	}
	user, err := s.users.UpsertFromClaims(r.Context(), claims, adminGroup)
	if err != nil {
		serverError(w, r, err, "user upsert failed")
		return
	}
	if err := s.sessions.Issue(w, auth.SessionFor(user.ID)); err != nil {
		serverError(w, r, err, "session create failed")
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *server) handleAuthLogout(w http.ResponseWriter, _ *http.Request) {
	if s.sessions == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "sessions are not configured")
		return
	}
	// The session lives in the cookie alone: clearing it is the logout.
	s.sessions.Clear(w)
	redirectURL := s.postLogoutRedirectURL
	if redirectURL == "" {
		redirectURL = "/"
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"redirectUrl": redirectURL})
}

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, user)
}

// updateMeRequest is the body of PATCH /api/me. responseLanguage is the coupled
// UI display + LLM answer language: 'en'/'de' pin both. There is no 'auto' — an
// unset profile is seeded from the browser locale on first visit, and the answer
// language directive already yields to an explicit in-message language request.
type updateMeRequest struct {
	ResponseLanguage string `json:"responseLanguage"`
}

// allowedResponseLanguages is the set of values PATCH /api/me accepts.
var allowedResponseLanguages = map[string]bool{"en": true, "de": true}

func (s *server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	if s.users == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "users are not configured")
		return
	}
	var body updateMeRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeDecodeError(w, err)
		return
	}
	if !allowedResponseLanguages[body.ResponseLanguage] {
		httpx.WriteError(w, http.StatusBadRequest, "unsupported response language")
		return
	}
	if err := s.users.UpdateResponseLanguage(r.Context(), user.ID, body.ResponseLanguage); err != nil {
		serverError(w, r, err, "update response language failed")
		return
	}
	user.ResponseLanguage = body.ResponseLanguage
	httpx.WriteJSON(w, http.StatusOK, user)
}

func (s *server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "users are not configured")
		return
	}
	users, err := s.users.ListUsers(r.Context())
	if err != nil {
		serverError(w, r, err, "list users failed")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, users)
}

// writeNotFound answers a missing resource, and equally one the caller does not
// own: the two must be indistinguishable.
func writeNotFound(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusNotFound, "not found")
}

// writeCacheableError is kit's error body under a Cache-Control of the caller's:
// httpx.WriteError marks every body no-store, and a miss the browser should
// remember (a favicon, a source image) must stay cacheable.
func writeCacheableError(w http.ResponseWriter, status int, message, cacheControl string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", cacheControl)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
