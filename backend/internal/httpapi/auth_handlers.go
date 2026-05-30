package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/trick77/spark/internal/auth"
)

const sessionTTL = 30 * 24 * time.Hour

func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "oidc is not configured")
		return
	}
	s.oidc.StartLogin(w, r)
}

func (s *server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil || s.users == nil || s.sessions == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "auth is not configured")
		return
	}
	claims, err := s.oidc.HandleCallback(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "oidc callback failed")
		return
	}
	user, err := s.users.UpsertFromClaims(r.Context(), claims, s.oidcAdminGroup)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "user upsert failed")
		return
	}
	session, err := s.sessions.Create(r.Context(), user.ID, sessionTTL)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "session create failed")
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
		writeJSONError(w, http.StatusInternalServerError, "list users failed")
		return
	}
	writeJSON(w, users)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
