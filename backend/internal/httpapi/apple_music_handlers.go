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
// Music connection: the same allowlist the library tools are restricted to.
func (s *server) appleMusicAllowed(user auth.User) bool {
	return s.appleMusic != nil && mcp.UsersAllow(s.appleMusicUsers, turn.MCPPrincipal(user))
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

// writeAppleMusic answers with the status, or maps a sidecar failure: a
// refused token is 422 with the reason and its code (malformed or rejected),
// Apple out of reach 502, the sidecar itself 503. The token never appears in a response or a log line.
func (s *server) writeAppleMusic(w http.ResponseWriter, r *http.Request, st tunewire.Status, err error) {
	var rejected *tunewire.RejectedError
	var upstream *tunewire.UpstreamError
	switch {
	case err == nil:
		w.Header().Set("Cache-Control", "no-store")
		httpx.WriteJSON(w, http.StatusOK, st)
	case errors.As(err, &rejected):
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": rejected.Reason, "code": rejected.Code})
	case errors.As(err, &upstream):
		httpx.WriteError(w, http.StatusBadGateway, upstream.Reason)
	default:
		slog.ErrorContext(r.Context(), "apple music sidecar failed", "err", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "Apple Music connection unavailable")
	}
}
