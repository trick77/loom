package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// faviconServer builds a server whose fetch client has no SSRF guard, so tests can
// use loopback httptest upstreams. The guard itself is unit-tested in
// TestGuardPublicAddr.
func faviconServer(t *testing.T) *server {
	t.Helper()
	return &server{faviconCacheDir: t.TempDir(), faviconClient: &http.Client{}}
}

func getFavicon(t *testing.T, s *server, target string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/favicon?u="+url.QueryEscape(target), nil)
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	s.handleFavicon(rec, req)
	return rec
}

func TestHandleFavicon_fetchesThenServesFromDisk(t *testing.T) {
	// Given an upstream that serves a PNG and counts hits.
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nfake"))
	}))
	defer upstream.Close()
	s := faviconServer(t)

	// When the favicon is requested twice.
	first := getFavicon(t, s, upstream.URL+"/favicon.ico", nil)
	second := getFavicon(t, s, upstream.URL+"/favicon.ico", nil)

	// Then both succeed, but the upstream is hit only once (second served from disk).
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("want 200/200, got %d/%d", first.Code, second.Code)
	}
	if got := first.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q, want image/png", got)
	}
	if got := first.Header().Get("Cache-Control"); got != faviconCacheControl {
		t.Fatalf("cache-control = %q, want %q", got, faviconCacheControl)
	}
	if first.Header().Get("ETag") == "" {
		t.Fatal("missing ETag")
	}
	if hits != 1 {
		t.Fatalf("upstream hits = %d, want 1 (second must be cached)", hits)
	}
}

func TestHandleFavicon_returns304OnIfNoneMatch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNGdata"))
	}))
	defer upstream.Close()
	s := faviconServer(t)

	first := getFavicon(t, s, upstream.URL+"/f.ico", nil)
	etag := first.Header().Get("ETag")

	second := getFavicon(t, s, upstream.URL+"/f.ico", http.Header{"If-None-Match": {etag}})
	if second.Code != http.StatusNotModified {
		t.Fatalf("want 304 on If-None-Match, got %d", second.Code)
	}
}

func TestGuardPublicAddr(t *testing.T) {
	for _, tc := range []struct {
		name    string
		addr    string
		blocked bool
	}{
		{"loopback-v4", "127.0.0.1:80", true},
		{"loopback-v6", "[::1]:80", true},
		{"private-10", "10.0.0.5:443", true},
		{"private-192", "192.168.1.1:443", true},
		{"link-local", "169.254.169.254:80", true}, // cloud metadata endpoint
		{"unspecified", "0.0.0.0:80", true},
		{"ula-v6", "[fd00::1]:80", true},
		{"public-v4", "93.184.216.34:443", false},
		{"unresolved", "example.com:443", true}, // must be a literal IP at dial time
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := guardPublicAddr("tcp", tc.addr, nil)
			if tc.blocked && err == nil {
				t.Fatalf("addr %q should be blocked", tc.addr)
			}
			if !tc.blocked && err != nil {
				t.Fatalf("addr %q should be allowed, got %v", tc.addr, err)
			}
		})
	}
}

func TestHandleFavicon_rejectsNonImage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer upstream.Close()
	s := faviconServer(t)

	rec := getFavicon(t, s, upstream.URL+"/f.ico", nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("non-image upstream must not return 200, got 200")
	}
}

func TestHandleFavicon_badRequests(t *testing.T) {
	s := faviconServer(t)
	for _, tc := range []struct {
		name, target string
	}{
		{"empty", ""},
		{"not-a-url", "://bad"},
		{"unsupported-scheme", "ftp://example.com/x.ico"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/favicon?u="+url.QueryEscape(tc.target), nil)
			rec := httptest.NewRecorder()
			s.handleFavicon(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("want 400, got %d", rec.Code)
			}
		})
	}
}

func TestFaviconCacheDirFor(t *testing.T) {
	if got := faviconCacheDirFor(""); got != "" {
		t.Fatalf("empty usersDir should disable cache, got %q", got)
	}
	if got := faviconCacheDirFor("/data/users"); !strings.HasSuffix(got, "/favicons") {
		t.Fatalf("cache dir = %q, want sibling favicons dir", got)
	}
}
