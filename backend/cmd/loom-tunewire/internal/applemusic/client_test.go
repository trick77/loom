package applemusic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDev hands out dev-1, then dev-2 after an invalidate.
type fakeDev struct {
	mu          sync.Mutex
	gen         int
	invalidated []string
	err         error
}

func (f *fakeDev) Token(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	return fmt.Sprintf("dev-%d", f.gen+1), nil
}

func (f *fakeDev) Invalidate(refused string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalidated = append(f.invalidated, refused)
	f.gen++
}

type recorded struct {
	method, path, auth, user, origin string
}

func newAPI(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*Client, *fakeDev, *[]recorded) {
	t.Helper()
	var mu sync.Mutex
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, recorded{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Media-User-Token"), r.Header.Get("Origin")})
		mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	dev := &fakeDev{}
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client(), Dev: dev, Sleep: func(context.Context, time.Duration) error { return nil }}
	return c, dev, &reqs
}

func TestPlaylists_followsPagesAndSendsThePlayerHeaders(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") == "" {
			_, _ = fmt.Fprint(w, `{"next":"/v1/me/library/playlists?offset=100","data":[
				{"id":"p.A","attributes":{"name":"Road trip","description":{"standard":"long drives"},"lastModifiedDate":"2026-10-01T10:00:00Z","artwork":{"url":"https://is1-ssl.mzstatic.com/a/{w}x{h}bb.jpg"}}}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"p.B","attributes":{"name":"Focus","artwork":{"url":"http://insecure/{w}x{h}.jpg"}}}]}`)
	})
	got, truncated, err := c.Playlists(context.Background(), "user-tok")
	if err != nil || truncated {
		t.Fatal(err, truncated)
	}
	if len(got) != 2 || got[0].Name != "Road trip" || got[0].Description != "long drives" || got[1].ID != "p.B" {
		t.Fatalf("playlists = %+v", got)
	}
	if got[0].Artwork != "https://is1-ssl.mzstatic.com/a/300x300bb.jpg" {
		t.Errorf("artwork = %q", got[0].Artwork)
	}
	if got[1].Artwork != "" {
		t.Errorf("non-https artwork kept: %q", got[1].Artwork)
	}
	if got[0].URL() != "https://music.apple.com/library/playlist/p.A" {
		t.Errorf("url = %q", got[0].URL())
	}
	r := *reqs
	if len(r) != 2 || !strings.Contains(r[1].path, "offset=100") || !strings.Contains(r[1].path, "limit=100") {
		t.Fatalf("requests = %+v", r)
	}
	for _, q := range r {
		if q.method != http.MethodGet || q.auth != "Bearer dev-1" || q.user != "user-tok" || q.origin != "https://music.apple.com" {
			t.Errorf("request %+v", q)
		}
	}
}

func TestPlaylists_emptyLibraryIs404(t *testing.T) {
	c, _, _ := newAPI(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	got, _, err := c.Playlists(context.Background(), "u")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

// endless serves full pages with a next link forever.
func endless(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	for i := range pageSize {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":"x.%d","attributes":{"name":"n"}}`, i)
	}
	_, _ = fmt.Fprintf(w, `{"next":"%s?offset=1","data":[%s]}`, r.URL.Path, b.String())
}

func TestPages_stopAtTheCapTruncated(t *testing.T) {
	c, _, reqs := newAPI(t, endless)
	list, truncated, err := c.Playlists(context.Background(), "u")
	if err != nil || !truncated || len(list) != MaxPlaylists || len(*reqs) != MaxPlaylists/pageSize {
		t.Fatalf("%d playlists, truncated %v, %d requests, %v", len(list), truncated, len(*reqs), err)
	}
	tracks, truncated, err := c.PlaylistTracks(context.Background(), "u", "p.1")
	if err != nil || !truncated || len(tracks) != MaxTracks {
		t.Fatalf("%d tracks, truncated %v, %v", len(tracks), truncated, err)
	}
}

func TestPlaylist(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/p.gone") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"p.A","attributes":{"name":"Road trip"}}]}`)
	})
	p, err := c.Playlist(context.Background(), "u", "p.A")
	if err != nil || p.ID != "p.A" || p.Name != "Road trip" || (*reqs)[0].path != "/v1/me/library/playlists/p.A" {
		t.Fatalf("playlist %+v, %v, %+v", p, err, *reqs)
	}
	if _, err := c.Playlist(context.Background(), "u", "p.gone"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}

func TestPlaylistTracks(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":[{"id":"i.1","attributes":{"name":"Song","artistName":"Band","albumName":"LP","durationInMillis":225000}}]}`)
	})
	got, truncated, err := c.PlaylistTracks(context.Background(), "u", "p.A/../x")
	if err != nil || truncated {
		t.Fatal(err, truncated)
	}
	if len(got) != 1 || got[0].Artist != "Band" || got[0].Duration != 225*time.Second {
		t.Fatalf("tracks = %+v", got)
	}
	if p := (*reqs)[0].path; !strings.HasPrefix(p, "/v1/me/library/playlists/p.A%2F..%2Fx/tracks") {
		t.Errorf("id not escaped: %s", p)
	}
}

func TestGet_401RefetchesTheDevTokenOnce(t *testing.T) {
	c, dev, reqs := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer dev-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = fmt.Fprint(w, `{"data":[{"id":"ch"}]}`)
	})
	sf, err := c.Storefront(context.Background(), "u")
	if err != nil || sf != "ch" {
		t.Fatalf("storefront = %q, %v", sf, err)
	}
	// storefront (401), probe without the user token (401), storefront again.
	r := *reqs
	if len(dev.invalidated) != 1 || dev.invalidated[0] != "dev-1" || len(r) != 3 || r[1].path != probePath || r[1].user != "" {
		t.Errorf("invalidated %v, requests %+v", dev.invalidated, r)
	}
}

// The probe passes: the user token is the one refused, and the developer
// token stays (no bundle download per expired call).
func TestGet_refusedUserToken(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		c, dev, _ := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == probePath {
				_, _ = fmt.Fprint(w, `{"data":[]}`)
				return
			}
			w.WriteHeader(status)
		})
		if _, err := c.Storefront(context.Background(), "u"); !errors.Is(err, ErrUserTokenExpired) {
			t.Errorf("%d: err = %v, want ErrUserTokenExpired", status, err)
		}
		if len(dev.invalidated) != 0 {
			t.Errorf("%d: developer token dropped: %v", status, dev.invalidated)
		}
	}
}

// A probe that is neither 200 nor 401/403 decides nothing: the developer token
// stays and the user token is not blamed.
func TestGet_inconclusiveProbe(t *testing.T) {
	c, dev, _ := newAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == probePath {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	_, err := c.Storefront(context.Background(), "u")
	if err == nil || errors.Is(err, ErrUserTokenExpired) || errors.Is(err, ErrDevTokenRefused) {
		t.Errorf("err = %v, want an inconclusive failure", err)
	}
	if len(dev.invalidated) != 0 {
		t.Errorf("developer token dropped: %v", dev.invalidated)
	}
}

func TestLibrarySize(t *testing.T) {
	for name, tc := range map[string]struct {
		status      int
		body        string
		want        int
		wantExact   bool
		wantRequest string
	}{
		"total":    {200, `{"data":[{}],"next":"/v1/x","meta":{"total":321}}`, 321, true, "/v1/me/library/playlists?limit=1"},
		"one page": {200, `{"data":[{}]}`, 1, true, ""},
		"no total": {200, `{"data":[{}],"next":"/v1/x"}`, 0, false, ""},
		"empty":    {404, ``, 0, true, ""},
	} {
		t.Run(name, func(t *testing.T) {
			c, _, reqs := newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			})
			n, exact, err := c.LibrarySize(context.Background(), "u")
			if err != nil || n != tc.want || exact != tc.wantExact {
				t.Errorf("got %d %v %v", n, exact, err)
			}
			if len(*reqs) != 1 || (tc.wantRequest != "" && (*reqs)[0].path != tc.wantRequest) {
				t.Errorf("requests %+v", *reqs)
			}
		})
	}
}

// Apple refuses the developer token even freshly fetched: not the user's
// token's fault.
func TestGet_refusedDevToken(t *testing.T) {
	c, dev, _ := newAPI(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	if _, err := c.Storefront(context.Background(), "u"); !errors.Is(err, ErrDevTokenRefused) {
		t.Errorf("library: %v", err)
	}
	if len(dev.invalidated) != 1 {
		t.Errorf("invalidated %v, want one refetch", dev.invalidated)
	}
	if _, err := c.Search(context.Background(), "ch", "x", SearchKinds, 1); !errors.Is(err, ErrDevTokenRefused) {
		t.Errorf("catalog: %v", err)
	}
}

func TestGet_429WaitsOnceThenFails(t *testing.T) {
	var waited []time.Duration
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"errors":[{"title":"Too Many Requests"}]}`)
	})
	c.Sleep = func(_ context.Context, d time.Duration) error { waited = append(waited, d); return nil }
	_, err := c.Storefront(context.Background(), "u")
	if err == nil || !strings.Contains(err.Error(), "Too Many Requests") {
		t.Fatalf("err = %v", err)
	}
	if len(waited) != 1 || waited[0] != maxRetryAfter || len(*reqs) != 2 {
		t.Errorf("waited %v, %d requests", waited, len(*reqs))
	}
}

func TestSearch_needsNoUserTokenAndKeepsKinds(t *testing.T) {
	c, _, reqs := newAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"results":{
			"songs":{"data":[{"attributes":{"name":"Bohemian Rhapsody","artistName":"Queen","albumName":"A Night at the Opera","releaseDate":"1975-10-31","durationInMillis":354000,"url":"https://music.apple.com/ch/song/1","artwork":{"url":"https://is1-ssl.mzstatic.com/s/{w}x{h}bb.{f}"}}}]},
			"artists":{"data":[{"attributes":{"name":"Queen","url":"https://music.apple.com/ch/artist/2"}}]}}}`)
	})
	got, err := c.Search(context.Background(), "ch", "bohemian rhapsody", SearchKinds, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Kind != "song" || got[0].Album != "A Night at the Opera" || got[1].Kind != "artist" {
		t.Fatalf("items = %+v", got)
	}
	if got[0].Artwork != "https://is1-ssl.mzstatic.com/s/300x300bb.jpg" {
		t.Errorf("artwork = %q", got[0].Artwork)
	}
	q := (*reqs)[0]
	if q.user != "" || !strings.HasPrefix(q.path, "/v1/catalog/ch/search?") || !strings.Contains(q.path, "term=bohemian+rhapsody") {
		t.Errorf("request %+v", q)
	}
}

func TestErrorsDoNotCarryTheQuery(t *testing.T) {
	c, _, _ := newAPI(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	_, err := c.Search(context.Background(), "ch", "secret words", SearchKinds, 5)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v", err)
	}
}

func TestDevTokenFailureIsReturned(t *testing.T) {
	c, dev, _ := newAPI(t, func(http.ResponseWriter, *http.Request) {})
	dev.err = errors.New("no bundle")
	if _, _, err := c.Playlists(context.Background(), "u"); err == nil || !strings.Contains(err.Error(), "no bundle") {
		t.Fatalf("err = %v", err)
	}
}
