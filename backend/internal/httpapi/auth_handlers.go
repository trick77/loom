package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/trick77/loom/internal/auth"
)

const sessionTTL = 30 * 24 * time.Hour

func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.devAuthClaims.Subject != "" {
		s.createSessionFromClaims(w, r, s.devAuthClaims, auth.DevAdminGroup)
		return
	}
	if s.oidc == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "oidc is not configured")
		return
	}
	s.oidc.StartLogin(w, r)
}

func (s *server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "oidc is not configured")
		return
	}
	claims, err := s.oidc.HandleCallback(r)
	if err != nil {
		http.Redirect(w, r, "/?auth_error=oidc_callback_failed", http.StatusFound)
		return
	}
	s.createSessionFromClaims(w, r, claims, s.oidcAdminGroup)
}

func (s *server) createSessionFromClaims(w http.ResponseWriter, r *http.Request, claims auth.Claims, adminGroup string) {
	if s.users == nil || s.sessions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "auth is not configured")
		return
	}
	user, err := s.users.UpsertFromClaims(r.Context(), claims, adminGroup)
	if err != nil {
		serverError(w, r, err, "user upsert failed")
		return
	}
	session, err := s.sessions.Create(r.Context(), user.ID, sessionTTL)
	if err != nil {
		serverError(w, r, err, "session create failed")
		return
	}
	http.SetCookie(w, s.sessions.CookieFor(session.Token, session.ExpiresAt))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "sessions are not configured")
		return
	}
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		_ = s.sessions.Revoke(r.Context(), cookie.Value)
	}
	http.SetCookie(w, s.sessions.ClearCookie())
	redirectURL := s.postLogoutRedirectURL
	if redirectURL == "" {
		redirectURL = "/"
	}
	writeJSON(w, map[string]string{"redirectUrl": redirectURL})
}

func (s *server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, user)
}

func (s *server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if s.users == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "users are not configured")
		return
	}
	users, err := s.users.ListUsers(r.Context())
	if err != nil {
		serverError(w, r, err, "list users failed")
		return
	}
	writeJSON(w, users)
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
