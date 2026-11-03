package httpapi

import (
	"context"
	"net/http"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/tunewire"
)

// maxAppleMusicTokenBody bounds PUT /api/me/apple-music: one short token.
const maxAppleMusicTokenBody = 16 << 10

// AppleMusicService is the loom-tunewire sidecar's token routes, each for
// one loom user's own account; *tunewire.Client implements it.
type AppleMusicService interface {
	Status(ctx context.Context, user string, check bool) (tunewire.Status, error)
	SetToken(ctx context.Context, user, token string) (tunewire.Status, error)
	ClearToken(ctx context.Context, user string) (tunewire.Status, error)
}

// appleMusicAllowed reports whether the account may connect Apple Music: it
// is on BACKEND_TUNEWIRE_USERS. Each listed user connects their own account.
func (s *server) appleMusicAllowed(user auth.User) bool {
	return featureAllowed(s.appleMusic != nil, s.appleMusicUsers, user)
}

func (s *server) appleMusicUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	return listedUser(w, r, s.appleMusic != nil, s.appleMusicUsers)
}

func (s *server) handleAppleMusicStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := s.appleMusicUser(w, r)
	if !ok {
		return
	}
	st, err := s.appleMusic.Status(r.Context(), user.ID, false)
	writeAppleMusic(w, r, st, err)
}

func (s *server) handleAppleMusicCheck(w http.ResponseWriter, r *http.Request) {
	user, ok := s.appleMusicUser(w, r)
	if !ok {
		return
	}
	st, err := s.appleMusic.Status(r.Context(), user.ID, true)
	writeAppleMusic(w, r, st, err)
}

func (s *server) handleAppleMusicSetToken(w http.ResponseWriter, r *http.Request) {
	user, ok := s.appleMusicUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSONBodyLimit(w, r, &body, maxAppleMusicTokenBody); err != nil {
		writeDecodeError(w, err)
		return
	}
	st, err := s.appleMusic.SetToken(r.Context(), user.ID, body.Token)
	writeAppleMusic(w, r, st, err)
}

func (s *server) handleAppleMusicClearToken(w http.ResponseWriter, r *http.Request) {
	user, ok := s.appleMusicUser(w, r)
	if !ok {
		return
	}
	st, err := s.appleMusic.ClearToken(r.Context(), user.ID)
	writeAppleMusic(w, r, st, err)
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

func writeAppleMusic(w http.ResponseWriter, r *http.Request, st tunewire.Status, err error) {
	writeSidecar(w, r, st, err, appleMusicStatus, "Apple Music connection unavailable")
}
