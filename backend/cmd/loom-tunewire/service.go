package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/cmd/loom-tunewire/internal/applemusic"
	"github.com/trick77/loom/cmd/loom-tunewire/internal/webtoken"
)

// maxUserTokenBytes bounds a pasted media-user-token; real ones are a few
// hundred bytes.
const maxUserTokenBytes = 8 << 10

var (
	errNotConnected = errors.New("no Apple Music token set")
	// errRejected: Apple refused a pasted token; the old one stays.
	errRejected = errors.New("apple music refused the token")
	// errMalformed: the pasted text cannot be a token at all.
	errMalformed = errors.New("that does not look like a media-user-token")
	// errStore: the token could not be written to /data; nothing changed.
	errStore = errors.New("could not store the Apple Music state")
)

// The kinds of the last error, so loom's settings page can word it without
// showing the sidecar's own (English, internal) text.
const (
	// errorKindBridge: Apple changed something the bridge relies on (the web
	// player's token); the user's token is not at fault.
	errorKindBridge = "bridge"
	// errorKindUpstream: Apple answered with an error or could not be reached.
	errorKindUpstream = "upstream"
)

// api is what the service needs from Apple; *applemusic.Client implements it.
type api interface {
	Storefront(ctx context.Context, userToken string) (string, error)
	LibrarySize(ctx context.Context, userToken string) (int, bool, error)
	Playlists(ctx context.Context, userToken string) ([]applemusic.Playlist, bool, error)
	Playlist(ctx context.Context, userToken, playlistID string) (applemusic.Playlist, error)
	PlaylistTracks(ctx context.Context, userToken, playlistID string) ([]applemusic.Track, bool, error)
	Search(ctx context.Context, storefront, term string, kinds []string, limit int) ([]applemusic.Item, error)
}

// service holds the one user's token and records what each call learns about
// it, so the settings page can say whether it still works.
type service struct {
	api        api
	store      fileStore
	storefront string
	now        func() time.Time

	// tokenMu serializes the token's changes (SetToken, ClearToken) whole,
	// Apple's check included: a disconnect that arrives during a paste's check
	// is applied after it, never undone by it.
	tokenMu sync.Mutex
	mu      sync.Mutex
	st      state
}

func newService(a api, store fileStore, storefront string) (*service, error) {
	st, err := store.load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	return &service{api: a, store: store, storefront: storefront, now: time.Now, st: st}, nil
}

// status is what loom's settings page shows. The token itself never leaves.
// PlaylistCount is absent while unknown.
type status struct {
	State         string    `json:"state"` // never, connected, expired, error
	Storefront    string    `json:"storefront,omitempty"`
	PlaylistCount *int      `json:"playlistCount,omitempty"`
	SetAt         time.Time `json:"setAt,omitzero"`
	LastOKAt      time.Time `json:"lastOkAt,omitzero"`
	LastError     string    `json:"lastError,omitempty"`
	ErrorKind     string    `json:"errorKind,omitempty"`
}

func (s *service) status() status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.st
	if st.UserToken == "" {
		return status{State: "never"}
	}
	out := status{Storefront: st.Storefront, PlaylistCount: st.PlaylistCount, SetAt: st.SetAt, LastOKAt: st.LastOKAt,
		LastError: st.LastError, ErrorKind: st.LastErrorKind}
	switch {
	case st.Expired:
		out.State = "expired"
	case st.LastError != "":
		out.State = "error"
	default:
		out.State = "connected"
	}
	return out
}

// check asks Apple once, with one request, so the status reflects its answer
// now.
func (s *service) check(ctx context.Context) status {
	tok, err := s.userToken()
	if err != nil {
		return s.status()
	}
	n, exact, err := s.api.LibrarySize(ctx, tok)
	if err != nil {
		slog.InfoContext(ctx, "tunewire check failed", "err", err)
	}
	s.record(tok, err, countOf(n, exact))
	return s.status()
}

func countOf(n int, exact bool) *int {
	if !exact {
		return nil
	}
	return &n
}

func (s *service) userToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.UserToken == "" {
		return "", errNotConnected
	}
	return s.st.UserToken, nil
}

// record notes the outcome of a library call made with token; count, when not
// nil, is the library's playlist count. A call that raced a token change says
// nothing about the new one and is dropped.
func (s *service) record(token string, err error, count *int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.UserToken != token {
		return
	}
	switch {
	case err == nil:
		before := s.st
		s.st.LastOKAt, s.st.LastError, s.st.LastErrorKind, s.st.Expired = s.now().UTC(), "", "", false
		if count != nil {
			s.st.PlaylistCount = count
		}
		// Only "last checked" moved: keep it in memory, write it to disk now
		// and then. Every tool call rewriting and fsyncing the file, under
		// the lock, would hold up the others for nothing.
		if sameButOK(before, s.st) && s.st.LastOKAt.Sub(before.LastOKAt) < okSaveInterval {
			return
		}
	case errors.Is(err, applemusic.ErrUserTokenExpired):
		s.st.Expired, s.st.LastError, s.st.LastErrorKind = true, err.Error(), ""
	case errors.Is(err, applemusic.ErrNotFound), transient(err):
		// Says nothing about the token: a missing playlist, a call that ran out
		// of time or was canceled.
		return
	default:
		s.st.LastError, s.st.LastErrorKind = err.Error(), errorKind(err)
	}
	if err := s.store.save(s.st); err != nil {
		slog.Error("tunewire: saving state failed", "err", err)
	}
}

// okSaveInterval is how stale the stored "last checked" may get before a
// successful call writes it.
const okSaveInterval = 10 * time.Minute

// sameButOK reports whether a and b differ in LastOKAt at most.
func sameButOK(a, b state) bool {
	a.LastOKAt, b.LastOKAt = time.Time{}, time.Time{}
	ac, bc := a.PlaylistCount, b.PlaylistCount
	a.PlaylistCount, b.PlaylistCount = nil, nil
	return a == b && (ac == nil) == (bc == nil) && (ac == nil || *ac == *bc)
}

// transient reports a failure that is the call's, not the connection's: a
// deadline, a cancel, a network timeout.
func transient(err error) bool {
	var ne net.Error
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &ne) && ne.Timeout())
}

func errorKind(err error) string {
	if errors.Is(err, applemusic.ErrDevTokenRefused) || errors.Is(err, webtoken.ErrNoBundle) ||
		errors.Is(err, webtoken.ErrNoToken) || errors.Is(err, webtoken.ErrStillRefused) {
		return errorKindBridge
	}
	return errorKindUpstream
}

// Playlists lists the library playlists; truncated says there are more.
func (s *service) Playlists(ctx context.Context) ([]applemusic.Playlist, bool, error) {
	tok, err := s.userToken()
	if err != nil {
		return nil, false, err
	}
	list, truncated, err := s.api.Playlists(ctx, tok)
	s.record(tok, err, countOf(len(list), err == nil && !truncated))
	return list, truncated, err
}

// Playlist reads one playlist's details by id.
func (s *service) Playlist(ctx context.Context, id string) (applemusic.Playlist, error) {
	tok, err := s.userToken()
	if err != nil {
		return applemusic.Playlist{}, err
	}
	p, err := s.api.Playlist(ctx, tok, id)
	s.record(tok, err, nil)
	return p, err
}

// PlaylistTracks reads one playlist; truncated says it holds more.
func (s *service) PlaylistTracks(ctx context.Context, id string) ([]applemusic.Track, bool, error) {
	tok, err := s.userToken()
	if err != nil {
		return nil, false, err
	}
	tracks, truncated, err := s.api.PlaylistTracks(ctx, tok, id)
	s.record(tok, err, nil)
	return tracks, truncated, err
}

// Search looks term up in the configured storefront's catalog: no user token.
func (s *service) Search(ctx context.Context, term string, kinds []string, limit int) ([]applemusic.Item, error) {
	return s.api.Search(ctx, s.storefront, term, kinds, limit)
}

// SetToken checks a pasted token with Apple (two requests) and stores it only
// if Apple accepts it; a refused one leaves the old token in place. When the
// caller gave up meanwhile (loom's timeout), nothing is stored: loom has told
// the user it failed.
func (s *service) SetToken(ctx context.Context, raw string) (status, error) {
	tok, err := normalizeToken(raw)
	if err != nil {
		return status{}, err
	}
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	sf, err := s.api.Storefront(ctx, tok)
	if errors.Is(err, applemusic.ErrUserTokenExpired) {
		return status{}, errRejected
	}
	if err != nil {
		return status{}, err
	}
	n, exact, countErr := s.api.LibrarySize(ctx, tok)
	if errors.Is(countErr, applemusic.ErrUserTokenExpired) {
		return status{}, errRejected
	}
	if err := ctx.Err(); err != nil {
		return status{}, err
	}
	s.mu.Lock()
	next := state{DevToken: s.st.DevToken, DevTokenExp: s.st.DevTokenExp,
		UserToken: tok, Storefront: sf, SetAt: s.now().UTC(), LastOKAt: s.now().UTC()}
	if countErr == nil {
		next.PlaylistCount = countOf(n, exact)
	} else if !transient(countErr) {
		next.LastError, next.LastErrorKind = countErr.Error(), errorKind(countErr)
	}
	err = s.store.save(next)
	if err == nil {
		s.st = next
	}
	s.mu.Unlock()
	if err != nil {
		slog.ErrorContext(ctx, "tunewire: saving the token failed", "err", err)
		return status{}, errStore
	}
	return s.status(), nil
}

// ClearToken forgets the user token, keeping the developer token.
func (s *service) ClearToken() (status, error) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	s.mu.Lock()
	next := state{DevToken: s.st.DevToken, DevTokenExp: s.st.DevTokenExp}
	err := s.store.save(next)
	if err == nil {
		s.st = next
	}
	s.mu.Unlock()
	if err != nil {
		slog.Error("tunewire: saving the cleared state failed", "err", err)
		return status{}, errStore
	}
	return s.status(), nil
}

// devToken is the developer token kept from an earlier run.
func (s *service) devToken() webtoken.Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	return webtoken.Token{Value: s.st.DevToken, Expires: s.st.DevTokenExp}
}

// keepDevToken stores a freshly fetched developer token across restarts.
func (s *service) keepDevToken(t webtoken.Token) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.DevToken, s.st.DevTokenExp = t.Value, t.Expires
	if err := s.store.save(s.st); err != nil {
		slog.Error("tunewire: saving state failed", "err", err)
	}
}

// normalizeToken accepts the cookie value as copied, with or without its
// name and quotes.
func normalizeToken(raw string) (string, error) {
	tok := strings.TrimSpace(raw)
	tok = strings.TrimPrefix(tok, "media-user-token=")
	tok = strings.TrimSuffix(tok, ";")
	tok = strings.Trim(tok, `"`)
	if tok == "" || len(tok) > maxUserTokenBytes {
		return "", errMalformed
	}
	if strings.ContainsFunc(tok, func(r rune) bool { return r <= ' ' || r == 0x7f || r == ';' || r == ',' || r > 0x7e }) {
		return "", errMalformed
	}
	return tok, nil
}
