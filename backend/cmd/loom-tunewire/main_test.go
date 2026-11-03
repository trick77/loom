package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/cmd/loom-tunewire/internal/applemusic"
	"github.com/trick77/loom/cmd/loom-tunewire/internal/webtoken"
	"github.com/trick77/loom/internal/mcpserver"
	"github.com/trick77/ship/kit/httpx"
)

const testToken = "0123456789abcdef0123456789abcdef"

// testUser is the loom user every test call is made for.
const testUser = "usr_test"

// userCtx is a tool call's context for testUser.
func userCtx() context.Context { return mcpserver.WithUser(context.Background(), testUser) }

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// fakeAPI answers like Apple for the token "good"; "bad" is refused.
type fakeAPI struct {
	mu        sync.Mutex
	playlists []applemusic.Playlist
	tracks    map[string][]applemusic.Track
	items     []applemusic.Item
	down      error // every call fails with it
	refuse    bool  // even "good" is refused now
	truncated bool  // lists say there is more
	searched  []string
	listCalls int
	sizeCalls int
	lastTok   string                    // the token the last call used
	sizeErr   error                     // LibrarySize alone fails with it
	sizeHook  func(ctx context.Context) // runs inside LibrarySize, mid-check

	// Listening (listening_test.go, releases_test.go).
	recent, heavy, added []applemusic.Item
	recs                 []applemusic.Recommendation
	mix                  map[string][]applemusic.Item // catalog playlist id → tracks
	listenErr            map[string]error             // method name → its failure
	libArtists           []applemusic.Artist
	libTruncated         bool
	catalogIDs           map[string]string          // library artist id → catalog id
	latest               map[string]applemusic.Item // catalog artist id → latest release
	latestErr            map[string]error
	latestHook           func(ctx context.Context, id string) // runs inside LatestRelease
	calls                map[string]int
	mixStorefront        string
	latestStorefront     string
	inflight, maxFlight  atomic.Int32
}

// listen runs the shared part of a listening call: the token check, the
// count, the method's own failure.
func (f *fakeAPI) listen(name, tok string) error {
	if err := f.check(tok); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[name]++
	return f.listenErr[name]
}

func (f *fakeAPI) called(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func (f *fakeAPI) RecentlyPlayed(_ context.Context, tok string) ([]applemusic.Item, error) {
	if err := f.listen("recent", tok); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recent, nil
}

func (f *fakeAPI) HeavyRotation(_ context.Context, tok string) ([]applemusic.Item, error) {
	if err := f.listen("heavy", tok); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.heavy, nil
}

func (f *fakeAPI) RecentlyAdded(_ context.Context, tok string) ([]applemusic.Item, error) {
	if err := f.listen("added", tok); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.added, nil
}

func (f *fakeAPI) Recommendations(_ context.Context, tok string) ([]applemusic.Recommendation, error) {
	if err := f.listen("recs", tok); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recs, nil
}

func (f *fakeAPI) CatalogPlaylistTracks(_ context.Context, tok, sf, id string) ([]applemusic.Item, error) {
	if err := f.listen("mix", tok); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mixStorefront = sf
	return f.mix[id], nil
}

func (f *fakeAPI) LibraryArtists(_ context.Context, tok string) ([]applemusic.Artist, bool, error) {
	if err := f.listen("artists", tok); err != nil {
		return nil, false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.libArtists, f.libTruncated, nil
}

func (f *fakeAPI) ArtistCatalogID(_ context.Context, tok, id string) (string, error) {
	if err := f.listen("catalogID", tok); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.catalogIDs[id], nil
}

// LatestRelease needs no user token, like Apple's.
func (f *fakeAPI) LatestRelease(ctx context.Context, sf, id string) (applemusic.Item, bool, error) {
	n := f.inflight.Add(1)
	defer f.inflight.Add(-1)
	for {
		m := f.maxFlight.Load()
		if n <= m || f.maxFlight.CompareAndSwap(m, n) {
			break
		}
	}
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls["latest"]++
	f.latestStorefront = sf
	hook, err, down := f.latestHook, f.latestErr[id], f.down
	it, ok := f.latest[id]
	f.mu.Unlock()
	if hook != nil {
		hook(ctx, id)
	}
	if down != nil {
		return applemusic.Item{}, false, down
	}
	if err != nil {
		return applemusic.Item{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return applemusic.Item{}, false, err
	}
	return it, ok, nil
}

func (f *fakeAPI) check(tok string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastTok = tok
	if f.down != nil {
		return f.down
	}
	// "good" and any "good-…" are real tokens: two users, two tokens.
	if !strings.HasPrefix(tok, "good") || f.refuse {
		return applemusic.ErrUserTokenExpired
	}
	return nil
}

func (f *fakeAPI) Storefront(_ context.Context, tok string) (string, error) {
	if err := f.check(tok); err != nil {
		return "", err
	}
	return "ch", nil
}

func (f *fakeAPI) Playlists(_ context.Context, tok string) ([]applemusic.Playlist, bool, error) {
	f.listCalls++
	if err := f.check(tok); err != nil {
		return nil, false, err
	}
	return f.playlists, f.truncated, nil
}

func (f *fakeAPI) LibrarySize(ctx context.Context, tok string) (int, bool, error) {
	f.sizeCalls++
	if f.sizeHook != nil {
		f.sizeHook(ctx)
	}
	if f.sizeErr != nil {
		return 0, false, f.sizeErr
	}
	if err := f.check(tok); err != nil {
		return 0, false, err
	}
	return len(f.playlists), !f.truncated, nil
}

func (f *fakeAPI) Playlist(_ context.Context, tok, id string) (applemusic.Playlist, error) {
	if err := f.check(tok); err != nil {
		return applemusic.Playlist{}, err
	}
	for _, p := range f.playlists {
		if p.ID == id {
			return p, nil
		}
	}
	return applemusic.Playlist{}, applemusic.ErrNotFound
}

func (f *fakeAPI) PlaylistTracks(_ context.Context, tok, id string) ([]applemusic.Track, bool, error) {
	if err := f.check(tok); err != nil {
		return nil, false, err
	}
	return f.tracks[id], f.truncated, nil
}

func (f *fakeAPI) Search(_ context.Context, sf, term string, kinds []string, limit int) ([]applemusic.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searched = append(f.searched, fmt.Sprintf("%s|%s|%s|%d", sf, term, strings.Join(kinds, ","), limit))
	if f.down != nil {
		return nil, f.down
	}
	return f.items, nil
}

func newTestService(t *testing.T) (*service, *fakeAPI, string) {
	t.Helper()
	dir := t.TempDir()
	api := &fakeAPI{
		playlists: []applemusic.Playlist{
			{ID: "p.1", Name: "Road Trip", Description: "long\n drives", LastModified: "2026-10-01T10:00:00Z", Artwork: "https://img/1.jpg"},
			{ID: "p.2", Name: "Focus"},
			{ID: "p.3", Name: "Focus Deep"},
			{ID: "p.4", Name: "Chill Evening"},
			{ID: "p.5", Name: "Chill Morning"},
		},
		tracks: map[string][]applemusic.Track{"p.1": {{Name: "Song", Artist: "Band", Album: "LP", Duration: 225 * time.Second}}},
	}
	svc, err := newService(api, fileStore{dir: dir}, "ch")
	if err != nil {
		t.Fatal(err)
	}
	return svc, api, dir
}

func TestConfig(t *testing.T) {
	env := map[string]string{"TUNEWIRE_TOKEN": testToken}
	get := func(k string) string { return env[k] }
	cfg, err := loadConfig(get)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.addr != defaultAddr || cfg.data != defaultDataDir || cfg.storefront != "ch" {
		t.Errorf("defaults: %+v", cfg)
	}
	env["TUNEWIRE_STOREFRONT"] = "US"
	if cfg, _ := loadConfig(get); cfg.storefront != "us" {
		t.Errorf("storefront = %q", cfg.storefront)
	}
	for _, bad := range []map[string]string{
		{"TUNEWIRE_TOKEN": "short"},
		{"TUNEWIRE_TOKEN": testToken + " x"},
		{"TUNEWIRE_TOKEN": testToken, "TUNEWIRE_STOREFRONT": "usa"},
	} {
		if _, err := loadConfig(func(k string) string { return bad[k] }); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestStore_atomicAndPrivate(t *testing.T) {
	dir := t.TempDir()
	s := fileStore{dir: dir}
	if st, err := s.load(); err != nil || len(st.Users) != 0 {
		t.Fatalf("empty load: %+v %v", st, err)
	}
	three := 3
	if err := s.save(state{Users: map[string]account{testUser: {UserToken: "tok", PlaylistCount: &three}}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, stateFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, %v", info.Mode(), err)
	}
	if st, _ := s.load(); st.Users[testUser].UserToken != "tok" || st.Users[testUser].PlaylistCount == nil || *st.Users[testUser].PlaylistCount != 3 {
		t.Errorf("round trip: %+v", st)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left: %v", entries)
	}
	// A failed write keeps the old file.
	if err := (fileStore{dir: filepath.Join(dir, "missing")}).save(state{Users: map[string]account{testUser: {UserToken: "new"}}}); err == nil {
		t.Error("save into a missing dir succeeded")
	}
	if st, _ := s.load(); st.Users[testUser].UserToken != "tok" {
		t.Errorf("old state lost: %+v", st)
	}
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newService(&fakeAPI{}, s, "ch"); err == nil {
		t.Error("corrupt state accepted")
	}
}

func TestNormalizeToken(t *testing.T) {
	for in, want := range map[string]string{
		"  abc  ":               "abc",
		"media-user-token=abc;": "abc",
		`"abc"`:                 "abc",
		"Ah+/=AbC09":            "Ah+/=AbC09",
	} {
		if got, err := normalizeToken(in); err != nil || got != want {
			t.Errorf("%q → %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "   ", "a b", "a;b", "a\nb", "ä", strings.Repeat("x", maxUserTokenBytes+1)} {
		if _, err := normalizeToken(bad); !errors.Is(err, errMalformed) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestService_tokenLifecycle(t *testing.T) {
	svc, api, dir := newTestService(t)
	ctx := userCtx()
	if st := svc.status(testUser); st.State != "never" {
		t.Fatalf("fresh: %+v", st)
	}
	if _, _, err := svc.Playlists(ctx); !errors.Is(err, errNotConnected) {
		t.Fatalf("no token: %v", err)
	}
	if _, err := svc.SetToken(ctx, testUser, "bad"); !errors.Is(err, errRejected) {
		t.Fatalf("bad token: %v", err)
	}
	if _, err := svc.SetToken(ctx, testUser, "a b"); !errors.Is(err, errMalformed) {
		t.Fatalf("malformed: %v", err)
	}
	st, err := svc.SetToken(ctx, testUser, " media-user-token=good ")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != "connected" || st.Storefront != "ch" || st.PlaylistCount == nil || *st.PlaylistCount != 5 || st.LastOKAt.IsZero() {
		t.Fatalf("after set: %+v", st)
	}
	// A refused replacement keeps the working token.
	if _, err := svc.SetToken(ctx, testUser, "bad"); !errors.Is(err, errRejected) {
		t.Fatal(err)
	}
	if tok, _ := svc.tokenOf(testUser); tok != "good" {
		t.Fatalf("token replaced by a refused one: %q", tok)
	}
	// Persisted, and never in the status JSON.
	b, _ := json.Marshal(svc.status(testUser))
	if strings.Contains(string(b), "good") {
		t.Errorf("status leaks the token: %s", b)
	}
	reloaded, _ := newService(api, fileStore{dir: dir}, "ch")
	if tok, _ := reloaded.tokenOf(testUser); tok != "good" {
		t.Errorf("not persisted: %q", tok)
	}

	api.down = errors.New("apple down")
	if st := svc.check(ctx, testUser); st.State != "error" || st.LastError != "apple down" {
		t.Errorf("down: %+v", st)
	}
	api.down, api.refuse = nil, true
	if st := svc.check(ctx, testUser); st.State != "expired" {
		t.Errorf("refused: %+v", st)
	}
	// Expired stays expired while Apple is unreachable: only a success clears it.
	api.refuse, api.down = false, errors.New("apple down")
	if st := svc.check(ctx, testUser); st.State != "expired" {
		t.Errorf("expired then down: %+v", st)
	}
	api.down = nil
	if st := svc.check(ctx, testUser); st.State != "connected" || st.LastError != "" {
		t.Errorf("recovered: %+v", st)
	}

	svc.keepDevToken(webtoken.Token{Value: "dev", Expires: time.Unix(2e9, 0)})
	if st, err := svc.ClearToken(testUser); err != nil || st.State != "never" {
		t.Errorf("cleared: %+v", st)
	}
	if svc.devToken().Value != "dev" {
		t.Error("clear dropped the developer token")
	}
}

func TestService_setTokenWhileAppleIsDown(t *testing.T) {
	svc, api, _ := newTestService(t)
	api.down = errors.New("timeout")
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err == nil || errors.Is(err, errRejected) {
		t.Fatalf("err = %v, want a plain failure", err)
	}
	if svc.status(testUser).State != "never" {
		t.Error("token stored without Apple's check")
	}
}

func TestService_staleRecordIsDropped(t *testing.T) {
	svc, _, _ := newTestService(t)
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	svc.record(testUser, "an older token", applemusic.ErrUserTokenExpired, nil)
	if svc.status(testUser).State != "connected" {
		t.Error("a call made with an old token marked the new one expired")
	}
}

func callTool(t *testing.T, h http.Handler, path, name, args string) (string, map[string]any) {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, name, args)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set(mcpserver.UserHeader, testUser)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var r struct {
		Result struct {
			Content           []struct{ Text string } `json:"content"`
			StructuredContent map[string]any          `json:"structuredContent"`
			IsError           bool                    `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil || len(r.Result.Content) == 0 {
		t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
	}
	text := r.Result.Content[0].Text
	if r.Result.IsError {
		text = "ERROR: " + text
	}
	return text, r.Result.StructuredContent
}

func TestLibraryTools(t *testing.T) {
	svc, api, _ := newTestService(t)
	h := newServer(config{token: testToken}, svc, "test")

	if text, _ := callTool(t, h, "/mcp/library", "list_playlists", `{}`); !strings.Contains(text, "ERROR: Apple Music is not connected") {
		t.Errorf("not connected: %s", text)
	}
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	text, sc := callTool(t, h, "/mcp/library", "list_playlists", `{}`)
	if !strings.Contains(text, "[1] Road Trip (id p.1) — long drives · changed 2026-10-01") || !strings.Contains(text, "[5] Chill Morning") {
		t.Errorf("list: %s", text)
	}
	sources := sc["sources"].([]any)
	first := sources[0].(map[string]any)
	if len(sources) != 5 || first["url"] != "https://music.apple.com/library/playlist/p.1" || first["image"] != "https://img/1.jpg" {
		t.Errorf("sources: %v", sources)
	}

	for _, tc := range []struct{ arg, want string }{
		{"p.1", `[1] Playlist "Road Trip" (id p.1), 1 tracks`},
		{"road trip", `Playlist "Road Trip"`},
		{"focus", `Playlist "Focus"`}, // exact beats partial
		{"chill", `ERROR: 2 playlists match "chill"; call again with one id: "Chill Evening" (p.4), "Chill Morning" (p.5)`},
		{"jazz", `ERROR: No playlist matches "jazz". The playlists are: "Road Trip", "Focus"`},
		{"", "ERROR: get_playlist needs a playlist id or name"},
	} {
		text, _ := callTool(t, h, "/mcp/library", "get_playlist", fmt.Sprintf(`{"playlist":%q}`, tc.arg))
		if !strings.Contains(text, tc.want) {
			t.Errorf("%q: %s", tc.arg, text)
		}
	}
	// An id is read directly, without paging through the library.
	before := api.listCalls
	text, _ = callTool(t, h, "/mcp/library", "get_playlist", `{"playlist":"p.1"}`)
	if !strings.Contains(text, "1. Song — Band · LP (3:45)") {
		t.Errorf("tracks: %s", text)
	}
	if api.listCalls != before {
		t.Errorf("an id paged through the library (%d list calls)", api.listCalls-before)
	}
	// An id-shaped name that is no id falls back to the name match.
	api.playlists = append(api.playlists, applemusic.Playlist{ID: "p.9", Name: "p.s. I love you"})
	if text, _ := callTool(t, h, "/mcp/library", "get_playlist", `{"playlist":"p.s."}`); !strings.Contains(text, `Playlist "p.s. I love you"`) {
		t.Errorf("id-shaped name: %s", text)
	}

	api.truncated = true
	if text, _ := callTool(t, h, "/mcp/library", "list_playlists", `{}`); !strings.Contains(text, "(the first 6; the library holds more)") {
		t.Errorf("truncated list: %s", text)
	}
	// A miss in a truncated list is not "no such playlist".
	if text, _ := callTool(t, h, "/mcp/library", "get_playlist", `{"playlist":"jazz"}`); !strings.Contains(text, "Only the first 6 playlists of the library could be searched") {
		t.Errorf("truncated miss: %s", text)
	}
	if text, _ := callTool(t, h, "/mcp/library", "get_playlist", `{"playlist":"p.1"}`); !strings.Contains(text, "the first 1 tracks (it holds more)") {
		t.Errorf("truncated tracks: %s", text)
	}
	api.truncated = false

	api.down = applemusic.ErrDevTokenRefused
	if text, _ := callTool(t, h, "/mcp/library", "list_playlists", `{}`); !strings.Contains(text, "The user's own sign-in is not the problem") {
		t.Errorf("dev refused: %s", text)
	}
	api.down = nil

	api.refuse = true
	if text, _ := callTool(t, h, "/mcp/library", "list_playlists", `{}`); !strings.Contains(text, "ERROR: Apple Music sign-in expired") {
		t.Errorf("expired: %s", text)
	}
	api.refuse, api.down = false, errors.New("boom")
	if text, _ := callTool(t, h, "/mcp/library", "list_playlists", `{}`); !strings.Contains(text, "ERROR: Apple Music is unavailable right now: boom") {
		t.Errorf("down: %s", text)
	}
}

func TestLibraryTools_manyPlaylistsCiteOnlyTheFirst20(t *testing.T) {
	svc, api, _ := newTestService(t)
	api.playlists = nil
	for i := range 25 {
		api.playlists = append(api.playlists, applemusic.Playlist{ID: fmt.Sprintf("p.%d", i), Name: fmt.Sprintf("List %d", i)})
	}
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	text, sc := callTool(t, newServer(config{token: testToken}, svc, "test"), "/mcp/library", "list_playlists", `{}`)
	if len(sc["sources"].([]any)) != maxSources || !strings.Contains(text, "[20] List 19") || !strings.Contains(text, "- List 20") {
		t.Errorf("text: %s", text)
	}
	text, _ = callTool(t, newServer(config{token: testToken}, svc, "test"), "/mcp/library", "get_playlist", `{"playlist":"nothing"}`)
	if !strings.Contains(text, "… and 0 more") && !strings.Contains(text, `"List 24"`) {
		t.Errorf("miss list: %s", text)
	}
}

func TestCatalogTool(t *testing.T) {
	svc, api, _ := newTestService(t)
	api.items = []applemusic.Item{
		{Kind: "song", Name: "Bohemian Rhapsody", Artist: "Queen", Album: "A Night at the Opera", Released: "1975-10-31", Duration: 354 * time.Second, URL: "https://music.apple.com/ch/song/1", Artwork: "https://img/s.jpg"},
		{Kind: "album", Name: "No Link", Artist: "Nobody"},
		{Kind: "artist", Name: "Queen", Artist: "Queen", URL: "https://music.apple.com/ch/artist/2"},
	}
	h := newServer(config{token: testToken}, svc, "test")
	// No user token needed.
	text, sc := callTool(t, h, "/mcp/catalog", "search_catalog", `{"query":" bohemian rhapsody "}`)
	// The hit without a link gets no [n], and the numbers keep matching.
	if !strings.Contains(text, "[1] song: Bohemian Rhapsody — Queen · A Night at the Opera · 1975 · 5:54") ||
		!strings.Contains(text, "- album: No Link — Nobody") || !strings.Contains(text, "[2] artist: Queen\n") {
		t.Errorf("text: %s", text)
	}
	if n := len(sc["sources"].([]any)); n != 2 {
		t.Errorf("%d sources, want 2", n)
	}
	s0 := sc["sources"].([]any)[0].(map[string]any)
	if s0["url"] != "https://music.apple.com/ch/song/1" || s0["image"] != "https://img/s.jpg" || s0["label"] != "Apple Music" {
		t.Errorf("source: %v", s0)
	}
	callTool(t, h, "/mcp/catalog", "search_catalog", `{"query":"x","type":"album","limit":50}`)
	if got := api.searched; got[0] != "ch|bohemian rhapsody|songs,albums,artists|5" || got[1] != "ch|x|albums|10" {
		t.Errorf("searched %v", got)
	}
	if text, _ := callTool(t, h, "/mcp/catalog", "search_catalog", `{"query":"x","type":"podcast"}`); !strings.HasPrefix(text, "ERROR: type must be") {
		t.Errorf("bad type: %s", text)
	}
	if text, _ := callTool(t, h, "/mcp/catalog", "search_catalog", `{"query":""}`); !strings.HasPrefix(text, "ERROR:") {
		t.Errorf("empty: %s", text)
	}
	api.items = nil
	if text, _ := callTool(t, h, "/mcp/catalog", "search_catalog", `{"query":"zzz"}`); !strings.Contains(text, `has nothing for "zzz"`) {
		t.Errorf("none: %s", text)
	}
	// The catalog endpoint does not offer library tools.
	if text, _ := callTool(t, h, "/mcp/library", "list_playlists", `{}`); !strings.Contains(text, "not connected") {
		t.Errorf("library: %s", text)
	}
}

func do(t *testing.T, h http.Handler, method, path, auth, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set(mcpserver.UserHeader, testUser)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestServer_routes(t *testing.T) {
	svc, api, _ := newTestService(t)
	h := newServer(config{token: testToken}, svc, "test")
	for _, r := range [][2]string{{"GET", "/status"}, {"PUT", "/token"}, {"DELETE", "/token"}, {"POST", "/mcp/library"}, {"POST", "/mcp/catalog"}} {
		if code, _ := do(t, h, r[0], r[1], "", ""); code != http.StatusUnauthorized {
			t.Errorf("%v without token: %d", r, code)
		}
		if code, _ := do(t, h, r[0], r[1], "wrong-token-wrong-token-wrong-tok", ""); code != http.StatusUnauthorized {
			t.Errorf("%v wrong token: %d", r, code)
		}
	}
	// kit's probe: loopback only, so a caller across the network gets 404.
	if code, _ := do(t, h, "GET", "/healthz", testToken, ""); code != http.StatusNotFound {
		t.Errorf("healthz from afar: %d", code)
	}
	if code, st := do(t, h, "GET", "/status", testToken, ""); code != 200 || st["state"] != "never" {
		t.Errorf("status: %d %v", code, st)
	}
	if code, _ := do(t, h, "PUT", "/token", testToken, `{`); code != http.StatusBadRequest {
		t.Errorf("bad json: %d", code)
	}
	if code, e := do(t, h, "PUT", "/token", testToken, `{"token":"bad"}`); code != http.StatusUnprocessableEntity || e["code"] != codeRejected {
		t.Errorf("rejected: %d %v", code, e)
	}
	if code, e := do(t, h, "PUT", "/token", testToken, `{"token":"a; b"}`); code != http.StatusUnprocessableEntity || e["code"] != codeMalformed {
		t.Errorf("malformed: %d %v", code, e)
	}
	if code, st := do(t, h, "PUT", "/token", testToken, `{"token":"good"}`); code != 200 || st["state"] != "connected" || st["playlistCount"] != float64(5) {
		t.Errorf("set: %d %v", code, st)
	}
	api.refuse = true
	if _, st := do(t, h, "GET", "/status?check=1", testToken, ""); st["state"] != "expired" {
		t.Errorf("check: %v", st)
	}
	api.refuse, api.down = false, errors.New("unreachable")
	if code, e := do(t, h, "PUT", "/token", testToken, `{"token":"good"}`); code != http.StatusBadGateway || !strings.Contains(e["error"].(string), "unreachable") || e["code"] != errorKindUpstream {
		t.Errorf("down: %d %v", code, e)
	}
	// The bridge broke: said so, not "Apple is down".
	api.down = webtoken.ErrNoBundle
	if code, e := do(t, h, "PUT", "/token", testToken, `{"token":"good"}`); code != http.StatusBadGateway || e["code"] != errorKindBridge {
		t.Errorf("bridge: %d %v", code, e)
	}
	if code, st := do(t, h, "DELETE", "/token", testToken, ""); code != 200 || st["state"] != "never" {
		t.Errorf("delete: %d %v", code, st)
	}
}

// The container's healthcheck reaches kit's /healthz over loopback without a
// token.
func TestHealthzOverLoopback(t *testing.T) {
	svc, _, _ := newTestService(t)
	srv := httptest.NewServer(newServer(config{token: testToken}, svc, "test"))
	defer srv.Close()
	if code := httpx.Healthcheck(httpx.LocalURL(strings.TrimPrefix(srv.URL, "http://"), "/healthz")); code != 0 {
		t.Errorf("healthcheck exit %d", code)
	}
}

func TestTransient(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("get: %w", context.DeadlineExceeded)} {
		if !transient(err) {
			t.Errorf("%v not transient", err)
		}
	}
	if transient(errors.New("apple returned 500")) || transient(applemusic.ErrUserTokenExpired) {
		t.Error("a real failure counted as transient")
	}
}

// A playlist longer than the text budget ends in a note, not a blunt cut.
func TestGetPlaylist_longListEndsInANote(t *testing.T) {
	svc, api, _ := newTestService(t)
	var long []applemusic.Track
	for i := range 300 {
		long = append(long, applemusic.Track{Name: fmt.Sprintf("A fairly long song title number %d", i), Artist: "Some Band With A Name", Album: "An Album Title Of Some Length", Duration: 200 * time.Second})
	}
	api.tracks["p.1"] = long
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	text, _ := callTool(t, newServer(config{token: testToken}, svc, "test"), "/mcp/library", "get_playlist", `{"playlist":"p.1"}`)
	if len(text) > trackTextBudget+200 || !strings.Contains(text, "more tracks not shown") || strings.Contains(text, "[output truncated]") {
		t.Errorf("len %d, tail %q", len(text), text[len(text)-120:])
	}
}

// Repeated successful calls do not rewrite the file each time.
func TestService_successWritesTheFileOnlyWhenSomethingChanged(t *testing.T) {
	svc, _, dir := newTestService(t)
	clock := now
	svc.now = func() time.Time { return clock }
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, stateFile)
	stat := func() time.Time {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.ModTime()
	}
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute)
	svc.check(context.Background(), testUser)
	if !stat().Equal(now) {
		t.Error("a check that changed only 'last checked' wrote the file")
	}
	clock = clock.Add(okSaveInterval)
	svc.check(context.Background(), testUser)
	if stat().Equal(now) {
		t.Error("a stale 'last checked' was never written")
	}
	if !sameButOK(account{UserToken: "a"}, account{UserToken: "a", LastOKAt: now}) || sameButOK(account{UserToken: "a"}, account{UserToken: "b"}) {
		t.Error("sameButOK")
	}
}

// A check is one request, not a read of the whole library.
func TestService_checkIsOneRequest(t *testing.T) {
	svc, api, _ := newTestService(t)
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	api.listCalls, api.sizeCalls = 0, 0
	if st := svc.check(context.Background(), testUser); st.State != "connected" || *st.PlaylistCount != 5 {
		t.Fatalf("check: %+v", st)
	}
	if api.listCalls != 0 || api.sizeCalls != 1 {
		t.Errorf("%d list calls, %d size calls", api.listCalls, api.sizeCalls)
	}
	// A refused developer token is the bridge's fault, worded as such.
	api.sizeErr = applemusic.ErrDevTokenRefused
	if st := svc.check(context.Background(), testUser); st.State != "error" || st.ErrorKind != errorKindBridge {
		t.Errorf("bridge: %+v", st)
	}
	api.sizeErr = errors.New("apple returned 500")
	if st := svc.check(context.Background(), testUser); st.ErrorKind != errorKindUpstream {
		t.Errorf("upstream: %+v", st)
	}
}

// loom gave up during the check: nothing is stored.
func TestService_setTokenStoresNothingOnceTheCallerGaveUp(t *testing.T) {
	svc, api, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	api.sizeHook = func(context.Context) { cancel() }
	if _, err := svc.SetToken(ctx, testUser, "good"); err == nil {
		t.Fatal("no error after the cancel")
	}
	if svc.status(testUser).State != "never" {
		t.Error("token stored although loom gave up")
	}
}

// The count could not be read in time: connected, count unknown, no error.
func TestService_setTokenWithoutACount(t *testing.T) {
	svc, api, _ := newTestService(t)
	api.sizeErr = context.DeadlineExceeded
	st, err := svc.SetToken(context.Background(), testUser, "good")
	if err != nil || st.State != "connected" || st.PlaylistCount != nil || st.LastError != "" {
		t.Fatalf("%+v %v", st, err)
	}
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), "playlistCount") {
		t.Errorf("unknown count sent: %s", b)
	}
}

// A token that cannot reach the disk is not reported as connected.
func TestService_storeFailure(t *testing.T) {
	svc, _, dir := newTestService(t)
	svc.store = fileStore{dir: filepath.Join(dir, "missing")}
	if _, err := svc.SetToken(context.Background(), testUser, "good"); !errors.Is(err, errStore) {
		t.Fatalf("set: %v", err)
	}
	if svc.status(testUser).State != "never" {
		t.Error("unsaved token in effect")
	}
	svc.store = fileStore{dir: dir}
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	svc.store = fileStore{dir: filepath.Join(dir, "missing")}
	if _, err := svc.ClearToken(testUser); !errors.Is(err, errStore) {
		t.Fatalf("clear: %v", err)
	}
	h := newServer(config{token: testToken}, svc, "test")
	if code, _ := do(t, h, "DELETE", "/token", testToken, ""); code != http.StatusInternalServerError {
		t.Errorf("DELETE: %d", code)
	}
	if code, _ := do(t, h, "PUT", "/token", testToken, `{"token":"good"}`); code != http.StatusInternalServerError {
		t.Errorf("PUT: %d", code)
	}
}

// A disconnect that arrives during a paste's check is applied after it, not
// undone by it.
func TestService_disconnectDuringAPasteWins(t *testing.T) {
	svc, api, _ := newTestService(t)
	cleared := make(chan struct{})
	api.sizeHook = func(context.Context) {
		api.sizeHook = nil
		go func() {
			_, _ = svc.ClearToken(testUser)
			close(cleared)
		}()
		time.Sleep(20 * time.Millisecond) // ClearToken is now waiting for the paste
	}
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	<-cleared
	if svc.status(testUser).State != "never" {
		t.Error("the paste undid the disconnect")
	}
}

// A call that ran out of time says nothing about the token.
func TestService_timeoutKeepsTheStatus(t *testing.T) {
	svc, api, _ := newTestService(t)
	if _, err := svc.SetToken(context.Background(), testUser, "good"); err != nil {
		t.Fatal(err)
	}
	api.down = context.DeadlineExceeded
	if st := svc.check(context.Background(), testUser); st.State != "connected" || st.LastError != "" {
		t.Errorf("after a timeout: %+v", st)
	}
}
