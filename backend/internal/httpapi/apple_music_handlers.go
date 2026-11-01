package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/tunewire"
	"github.com/trick77/loom/internal/turn"
	"github.com/trick77/ship/kit/httpx"
)

// maxAppleMusicTokenBody bounds PUT /api/me/apple-music: one short token.
const maxAppleMusicTokenBody = 16 << 10

// AppleMusicService is the loom-tunewire sidecar's token routes;
// *tunewire.Client implements it.
type AppleMusicService interface {
	Status(ctx context.Context, check bool) (tunewire.Status, error)
	SetToken(ctx context.Context, token string) (tunewire.Status, error)
	ClearToken(ctx context.Context) (tunewire.Status, error)
}

// appleMusicAllowed reports whether the account owns the sidecar's Apple
// Music connection: the one user the library tools are restricted to. An
// unset owner matches nobody.
func (s *server) appleMusicAllowed(user auth.User) bool {
	return s.appleMusic != nil && mcp.UsersAllow([]string{s.appleMusicOwner}, turn.MCPPrincipal(user))
}

// appleMusicUser returns the signed-in owner. Anyone else gets 404: the
// connection holds one person's token, and another account must not learn
// its state, let alone replace the token with theirs.
func (s *server) appleMusicUser(w http.ResponseWriter, r *http.Request) bool {
	user, ok := currentUser(w, r)
	if !ok {
		return false
	}
	if !s.appleMusicAllowed(user) {
		httpx.WriteError(w, http.StatusNotFound, "not found")
		return false
	}
	return true
}

func (s *server) handleAppleMusicStatus(w http.ResponseWriter, r *http.Request) {
	if !s.appleMusicUser(w, r) {
		return
	}
	st, err := s.appleMusic.Status(r.Context(), false)
	s.writeAppleMusic(w, r, st, err)
}

func (s *server) handleAppleMusicCheck(w http.ResponseWriter, r *http.Request) {
	if !s.appleMusicUser(w, r) {
		return
	}
	st, err := s.appleMusic.Status(r.Context(), true)
	s.writeAppleMusic(w, r, st, err)
}

func (s *server) handleAppleMusicSetToken(w http.ResponseWriter, r *http.Request) {
	if !s.appleMusicUser(w, r) {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSONBodyLimit(w, r, &body, maxAppleMusicTokenBody); err != nil {
		writeDecodeError(w, err)
		return
	}
	st, err := s.appleMusic.SetToken(r.Context(), body.Token)
	s.writeAppleMusic(w, r, st, err)
}

func (s *server) handleAppleMusicClearToken(w http.ResponseWriter, r *http.Request) {
	if !s.appleMusicUser(w, r) {
		return
	}
	st, err := s.appleMusic.ClearToken(r.Context())
	s.writeAppleMusic(w, r, st, err)
}

// appleMusicStatus maps a sidecar code to the response status: a refused
// token 422, Apple or the bridge 502, the sidecar's storage 500, the rest 503.
var appleMusicStatus = map[string]int{
	tunewire.CodeMalformed: http.StatusUnprocessableEntity,
	tunewire.CodeRejected:  http.StatusUnprocessableEntity,
	tunewire.CodeBridge:    http.StatusBadGateway,
	tunewire.CodeUpstream:  http.StatusBadGateway,
	tunewire.CodeStorage:   http.StatusInternalServerError,
	tunewire.CodeConfig:    http.StatusServiceUnavailable,
}

// writeAppleMusic answers with the status, or maps a sidecar failure to its
// HTTP status with {"error", "code"}: the page words it by code. A sidecar out
// of reach is 503 "unavailable". The token never appears in a response or a
// log line.
func (s *server) writeAppleMusic(w http.ResponseWriter, r *http.Request, st tunewire.Status, err error) {
	var e *tunewire.Error
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusOK, st)
	case errors.As(err, &e):
		status, ok := appleMusicStatus[e.Code]
		if !ok {
			status = http.StatusServiceUnavailable
		}
		if status >= http.StatusInternalServerError && status != http.StatusBadGateway {
			slog.ErrorContext(r.Context(), "apple music sidecar failed", "err", err)
		}
		httpx.WriteJSON(w, status, map[string]string{"error": e.Reason, "code": e.Code})
	default:
		slog.ErrorContext(r.Context(), "apple music sidecar failed", "err", err)
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Apple Music connection unavailable", "code": "unavailable"})
	}
}
