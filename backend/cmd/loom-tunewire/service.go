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

	"github.com/trick77/toolkit/applemusic"
	"github.com/trick77/toolkit/mcpserver"
)

// maxUserTokenBytes bounds a pasted media-user-token; real ones are a few
// hundred bytes.
const maxUserTokenBytes = 8 << 10

// maxUserIDBytes bounds a loom user id; loom's are short opaque strings.
const maxUserIDBytes = 128

var (
	errNotConnected = errors.New("no Apple Music token set")
	// errNoUser: loom sent no user (loomuser.Header). Every account is one
	// person's, so a call that names nobody reads nothing.
	errNoUser = errors.New("no loom user given")
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
	RecentlyPlayed(ctx context.Context, userToken string) ([]applemusic.Item, error)
	HeavyRotation(ctx context.Context, userToken string) ([]applemusic.Item, error)
	RecentlyAdded(ctx context.Context, userToken string) ([]applemusic.Item, error)
	Recommendations(ctx context.Context, userToken string) ([]applemusic.Recommendation, error)
	CatalogPlaylistTracks(ctx context.Context, userToken, storefront, playlistID string) ([]applemusic.Item, error)
	LibraryArtists(ctx context.Context, userToken string) ([]applemusic.Artist, bool, error)
	ArtistCatalogID(ctx context.Context, userToken, libraryID string) (string, error)
	LatestRelease(ctx context.Context, storefront, artistID string) (applemusic.Item, bool, error)
}

// service holds one Apple Music account per loom user and records what each
// call learns about it, so that user's settings page can say whether it still
// works. A call reads only its caller's account (mcpserver.User).
type service struct {
	api        api
	store      fileStore
	storefront string
	now        func() time.Time

	// tokenLocks serialize each user's token changes (SetToken, ClearToken)
	// whole, Apple's check included: a disconnect that arrives during a paste's
	// check is applied after it, never undone by it. Per user: one user's slow
	// check never holds up another's.
	tokenLocks sync.Map // loom user id → *sync.Mutex
	mu         sync.Mutex
	st         state

	// new_releases' memory (releases.go): latest releases per artist (catalog
	// data, shared), each user's library artists, and how long the artist
	// lookups may run.
	releases      releaseCache
	artists       artistsCache
	releaseBudget time.Duration
}

func newService(a api, store fileStore, storefront string) (*service, error) {
	st, err := store.load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}
	return &service{api: a, store: store, storefront: storefront, now: time.Now, st: st, releaseBudget: releaseBudget}, nil
}

// checkUser validates a loom user id from the request.
func checkUser(user string) error {
	if user == "" || len(user) > maxUserIDBytes {
		return errNoUser
	}
	return nil
}

// callerOf is the loom user a tool call is made for.
func callerOf(ctx context.Context) (string, error) {
	user := mcpserver.User(ctx)
	return user, checkUser(user)
}

// status is what loom's settings page shows its user. The token itself never
// leaves. PlaylistCount is absent while unknown.
type status struct {
	State         string    `json:"state"` // never, connected, expired, error
	Storefront    string    `json:"storefront,omitempty"`
	PlaylistCount *int      `json:"playlistCount,omitempty"`
	SetAt         time.Time `json:"setAt,omitzero"`
	LastOKAt      time.Time `json:"lastOkAt,omitzero"`
	LastError     string    `json:"lastError,omitempty"`
	ErrorKind     string    `json:"errorKind,omitempty"`
}

func (s *service) status(user string) status {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.st.Users[user]
	if !ok || a.UserToken == "" {
		return status{State: "never"}
	}
	out := status{Storefront: a.Storefront, PlaylistCount: a.PlaylistCount, SetAt: a.SetAt, LastOKAt: a.LastOKAt,
		LastError: a.LastError, ErrorKind: a.LastErrorKind}
	switch {
	case a.Expired:
		out.State = "expired"
	case a.LastError != "":
		out.State = "error"
	default:
		out.State = "connected"
	}
	return out
}

// accounts is how many loom users have a token stored.
func (s *service) accounts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.Users)
}

// check asks Apple once, with one request, so the user's status reflects its
// answer now.
func (s *service) check(ctx context.Context, user string) status {
	tok, err := s.tokenOf(user)
	if err != nil {
		return s.status(user)
	}
	n, exact, err := s.api.LibrarySize(ctx, tok)
	if err != nil {
		slog.InfoContext(ctx, "tunewire check failed", "err", err)
	}
	s.record(user, tok, err, countOf(n, exact))
	return s.status(user)
}

func countOf(n int, exact bool) *int {
	if !exact {
		return nil
	}
	return &n
}

// tokenOf is user's own token.
func (s *service) tokenOf(user string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.st.Users[user]
	if !ok || a.UserToken == "" {
		return "", errNotConnected
	}
	return a.UserToken, nil
}

// userToken is the calling user's token.
func (s *service) userToken(ctx context.Context) (string, string, error) {
	user, err := callerOf(ctx)
	if err != nil {
		return "", "", err
	}
	tok, err := s.tokenOf(user)
	return user, tok, err
}

// record notes the outcome of a library call made with user's token; count,
// when not nil, is the library's playlist count. A call that raced a token
// change says nothing about the new one and is dropped.
func (s *service) record(user, token string, err error, count *int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.st.Users[user]
	if !ok || a.UserToken != token {
		return
	}
	switch {
	case err == nil:
		before := a
		a.LastOKAt, a.LastError, a.LastErrorKind, a.Expired = s.now().UTC(), "", "", false
		if count != nil {
			a.PlaylistCount = count
		}
		// Only "last checked" moved: keep it in memory, write it to disk now
		// and then. Every tool call rewriting and fsyncing the file, under
		// the lock, would hold up the others for nothing.
		if sameButOK(before, a) && a.LastOKAt.Sub(before.LastOKAt) < okSaveInterval {
			s.st.Users[user] = a
			return
		}
	case errors.Is(err, applemusic.ErrUserTokenExpired):
		a.Expired, a.LastError, a.LastErrorKind = true, err.Error(), ""
	case errors.Is(err, applemusic.ErrNotFound), transient(err):
		// Says nothing about the token: a missing playlist, a call that ran out
		// of time or was canceled.
		return
	default:
		a.LastError, a.LastErrorKind = err.Error(), errorKind(err)
	}
	s.st.Users[user] = a
	if err := s.store.save(s.st); err != nil {
		slog.Error("tunewire: saving state failed", "err", err)
	}
}

// okSaveInterval is how stale the stored "last checked" may get before a
// successful call writes it.
const okSaveInterval = 10 * time.Minute

// sameButOK reports whether a and b differ in LastOKAt at most.
func sameButOK(a, b account) bool {
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
	if errors.Is(err, applemusic.ErrDevTokenRefused) || errors.Is(err, applemusic.ErrNoBundle) ||
		errors.Is(err, applemusic.ErrNoToken) || errors.Is(err, applemusic.ErrStillRefused) {
		return errorKindBridge
	}
	return errorKindUpstream
}

// Playlists lists the caller's library playlists; truncated says there are
// more.
func (s *service) Playlists(ctx context.Context) ([]applemusic.Playlist, bool, error) {
	user, tok, err := s.userToken(ctx)
	if err != nil {
		return nil, false, err
	}
	list, truncated, err := s.api.Playlists(ctx, tok)
	s.record(user, tok, err, countOf(len(list), err == nil && !truncated))
	return list, truncated, err
}

// Playlist reads one of the caller's playlists by id.
func (s *service) Playlist(ctx context.Context, id string) (applemusic.Playlist, error) {
	return withToken(ctx, s, func(tok string) (applemusic.Playlist, error) { return s.api.Playlist(ctx, tok, id) })
}

// PlaylistTracks reads one of the caller's playlists; truncated says it holds
// more.
func (s *service) PlaylistTracks(ctx context.Context, id string) ([]applemusic.Track, bool, error) {
	user, tok, err := s.userToken(ctx)
	if err != nil {
		return nil, false, err
	}
	tracks, truncated, err := s.api.PlaylistTracks(ctx, tok, id)
	s.record(user, tok, err, nil)
	return tracks, truncated, err
}

// withToken runs a library call with the caller's token and records what it
// learned about that token.
func withToken[T any](ctx context.Context, s *service, call func(tok string) (T, error)) (T, error) {
	user, tok, err := s.userToken(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	v, err := call(tok)
	s.record(user, tok, err, nil)
	return v, err
}

// ownerStorefront is the caller's own storefront, learned when the token was
// set, else the configured one: the caller's tools read the catalog the
// caller listens in.
func (s *service) ownerStorefront(ctx context.Context) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.st.Users[mcpserver.User(ctx)]; ok && a.Storefront != "" {
		return a.Storefront
	}
	return s.storefront
}

// RecentlyPlayed lists the caller's recently played tracks.
func (s *service) RecentlyPlayed(ctx context.Context) ([]applemusic.Item, error) {
	return withToken(ctx, s, func(tok string) ([]applemusic.Item, error) { return s.api.RecentlyPlayed(ctx, tok) })
}

// HeavyRotation lists what the caller played most lately.
func (s *service) HeavyRotation(ctx context.Context) ([]applemusic.Item, error) {
	return withToken(ctx, s, func(tok string) ([]applemusic.Item, error) { return s.api.HeavyRotation(ctx, tok) })
}

// RecentlyAdded lists what the caller added to the library lately.
func (s *service) RecentlyAdded(ctx context.Context) ([]applemusic.Item, error) {
	return withToken(ctx, s, func(tok string) ([]applemusic.Item, error) { return s.api.RecentlyAdded(ctx, tok) })
}

// Recommendations lists Apple's personal picks for the caller.
func (s *service) Recommendations(ctx context.Context) ([]applemusic.Recommendation, error) {
	return withToken(ctx, s, func(tok string) ([]applemusic.Recommendation, error) { return s.api.Recommendations(ctx, tok) })
}

// MixTracks reads a catalog playlist (a personal mix) in the caller's
// storefront, with the caller's token.
func (s *service) MixTracks(ctx context.Context, playlistID string) ([]applemusic.Item, error) {
	sf := s.ownerStorefront(ctx)
	return withToken(ctx, s, func(tok string) ([]applemusic.Item, error) {
		return s.api.CatalogPlaylistTracks(ctx, tok, sf, playlistID)
	})
}

// ArtistCatalogID maps a library artist to its catalog artist; "" for none.
func (s *service) ArtistCatalogID(ctx context.Context, libraryID string) (string, error) {
	return withToken(ctx, s, func(tok string) (string, error) { return s.api.ArtistCatalogID(ctx, tok, libraryID) })
}

// Search looks term up in the configured storefront's catalog: no user token.
func (s *service) Search(ctx context.Context, term string, kinds []string, limit int) ([]applemusic.Item, error) {
	return s.api.Search(ctx, s.storefront, term, kinds, limit)
}

// SetToken checks user's pasted token with Apple (two requests) and stores it
// as that user's only if Apple accepts it; a refused one leaves the old token
// in place. When the caller gave up meanwhile (loom's timeout), nothing is
// stored: loom has told the user it failed.
func (s *service) SetToken(ctx context.Context, user, raw string) (status, error) {
	if err := checkUser(user); err != nil {
		return status{}, err
	}
	tok, err := normalizeToken(raw)
	if err != nil {
		return status{}, err
	}
	unlock := s.lockTokens(user)
	defer unlock()
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
	a := account{UserToken: tok, Storefront: sf, SetAt: s.now().UTC(), LastOKAt: s.now().UTC()}
	if countErr == nil {
		a.PlaylistCount = countOf(n, exact)
	} else if !transient(countErr) {
		a.LastError, a.LastErrorKind = countErr.Error(), errorKind(countErr)
	}
	s.mu.Lock()
	next := s.st.clone()
	next.Users[user] = a
	err = s.store.save(next)
	if err == nil {
		s.st = next
	}
	s.mu.Unlock()
	if err != nil {
		slog.ErrorContext(ctx, "tunewire: saving the token failed", "err", err)
		return status{}, errStore
	}
	s.artists.forget(user)
	return s.status(user), nil
}

// ClearToken forgets user's token; every other account stays.
func (s *service) ClearToken(user string) (status, error) {
	if err := checkUser(user); err != nil {
		return status{}, err
	}
	unlock := s.lockTokens(user)
	defer unlock()
	s.mu.Lock()
	next := s.st.clone()
	delete(next.Users, user)
	err := s.store.save(next)
	if err == nil {
		s.st = next
	}
	s.mu.Unlock()
	if err != nil {
		slog.Error("tunewire: saving the cleared state failed", "err", err)
		return status{}, errStore
	}
	s.artists.forget(user)
	return s.status(user), nil
}

// lockTokens takes user's token-change lock and returns its release. The
// locks stay: one small mutex per loom user who ever connected.
func (s *service) lockTokens(user string) func() {
	m, _ := s.tokenLocks.LoadOrStore(user, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// devToken is the developer token kept from an earlier run.
func (s *service) devToken() applemusic.Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	return applemusic.Token{Value: s.st.DevToken, Expires: s.st.DevTokenExp}
}

// keepDevToken stores a freshly fetched developer token across restarts.
func (s *service) keepDevToken(t applemusic.Token) {
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
