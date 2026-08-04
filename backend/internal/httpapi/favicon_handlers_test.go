package httpapi

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// faviconServer builds a server whose fetch client has no SSRF guard, so tests can
// use loopback httptest upstreams. The guard itself is unit-tested in
// TestGuardPublicAddr. faviconService is stubbed to "" so the last-resort branch
// never reaches the real Google service.
func faviconServer(t *testing.T) *server {
	t.Helper()
	return &server{
		faviconCacheDir: t.TempDir(),
		faviconClient:   &http.Client{},
		faviconService:  func(string) string { return "" },
	}
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

// writePNG writes a tiny fake PNG.
func writePNG(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nfake"))
}

// writePNGBytes serves an already-encoded PNG.
func writePNGBytes(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(body)
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// placeholderIconPNG mimics the favicon service's "no icon for this site" answer: a
// pure-black glyph on a fully transparent background, invisible on the dark UI.
func placeholderIconPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 8; y < 24; y++ {
		for x := 8; x < 24; x++ {
			img.Set(x, y, color.RGBA{A: 255})
		}
	}
	return encodePNG(t, img)
}

// realIconPNG mimics a genuine service icon: opaque, mostly light, with a dark mark.
func realIconPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	for y := 10; y < 22; y++ {
		for x := 10; x < 22; x++ {
			img.Set(x, y, color.RGBA{R: 36, G: 41, B: 46, A: 255})
		}
	}
	return encodePNG(t, img)
}

func TestIsVisibleServiceIcon(t *testing.T) {
	if isVisibleServiceIcon(placeholderIconPNG(t)) {
		t.Fatal("black-on-transparent placeholder must not count as a visible icon")
	}
	if !isVisibleServiceIcon(realIconPNG(t)) {
		t.Fatal("an opaque light icon must count as visible")
	}
	// Fully transparent: nothing painted at all.
	if isVisibleServiceIcon(encodePNG(t, image.NewRGBA(image.Rect(0, 0, 16, 16)))) {
		t.Fatal("a fully transparent image must not count as a visible icon")
	}
	// Undecodable bytes are accepted rather than treated as a placeholder.
	if !isVisibleServiceIcon([]byte("\x89PNG\r\n\x1a\nfake")) {
		t.Fatal("undecodable bytes must be accepted unchanged")
	}
}

// serviceFaviconServer builds a server whose last-resort favicon service points at
// serviceURL instead of Google's.
func serviceFaviconServer(t *testing.T, serviceURL string) *server {
	t.Helper()
	s := faviconServer(t)
	s.faviconService = func(string) string { return serviceURL }
	return s
}

// bareSite serves a page with no icon links and 404s every icon path, so resolution
// always falls through to the last-resort service.
func bareSite() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><head></head></html>"))
			return
		}
		http.NotFound(w, r)
	}
}

func TestHandleFavicon_rejectsServicePlaceholderIcon(t *testing.T) {
	// Given a site whose every icon candidate 404s (bot protection, in practice) and a
	// favicon service that answers 200 with its placeholder globe.
	placeholder := placeholderIconPNG(t)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writePNGBytes(w, placeholder)
	}))
	defer service.Close()
	upstream := httptest.NewServer(bareSite())
	defer upstream.Close()
	s := serviceFaviconServer(t, service.URL+"/s2")

	// When the icon is requested.
	rec := getFavicon(t, s, upstream.URL+"/wiki/Page", nil)

	// Then it is treated as "no icon", so the frontend draws its letter avatar
	// instead of an invisible black glyph.
	if rec.Code == http.StatusOK {
		t.Fatal("the service placeholder must not be served as an icon")
	}
	host := mustHost(t, upstream.URL)
	if _, _, ok := s.faviconCached(faviconCacheKey(host)); ok {
		t.Fatal("the placeholder must not be cached")
	}
}

func TestHandleFavicon_acceptsRealServiceIcon(t *testing.T) {
	// Given the same bare site, but the service has a genuine icon for it.
	real := realIconPNG(t)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writePNGBytes(w, real)
	}))
	defer service.Close()
	upstream := httptest.NewServer(bareSite())
	defer upstream.Close()
	s := serviceFaviconServer(t, service.URL+"/s2")

	rec := getFavicon(t, s, upstream.URL+"/", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 for a real service icon, got %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), real) {
		t.Fatal("served bytes differ from the service icon")
	}
}

func TestHandleFavicon_keepsDarkIconFromTheSiteItself(t *testing.T) {
	// The visibility gate is scoped to the last-resort service: a site that ships an
	// all-black mark of its own still gets its real icon, not a letter avatar.
	dark := placeholderIconPNG(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><head></head></html>"))
		case "/favicon.ico":
			writePNGBytes(w, dark)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := faviconServer(t)

	rec := getFavicon(t, s, upstream.URL+"/", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 (site's own icon is never screened), got %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), dark) {
		t.Fatal("served bytes differ from the site's own icon")
	}
}

func TestHandleFavicon_remembersUnresolvableSite(t *testing.T) {
	// Given a site where every candidate 404s.
	var probes int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes++
		bareSite()(w, r)
	}))
	defer upstream.Close()
	s := faviconServer(t)

	// When it is requested twice.
	first := getFavicon(t, s, upstream.URL+"/", nil)
	after := probes
	second := getFavicon(t, s, upstream.URL+"/other", nil)

	// Then the second request is answered from the miss marker without re-probing,
	// and both tell the browser to stop re-asking for a while.
	if first.Code == http.StatusOK || second.Code == http.StatusOK {
		t.Fatalf("want non-2xx twice, got %d/%d", first.Code, second.Code)
	}
	if probes != after {
		t.Fatalf("second request re-probed the site (%d extra requests)", probes-after)
	}
	if got := second.Header().Get("Cache-Control"); got != faviconMissCacheControl {
		t.Fatalf("cache-control = %q, want %q", got, faviconMissCacheControl)
	}
}

func TestHandleFavicon_reResolvesAfterMissExpires(t *testing.T) {
	// Given a site that has no icon on the first pass but gains one later.
	var hasIcon bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" && hasIcon {
			writePNG(w)
			return
		}
		bareSite()(w, r)
	}))
	defer upstream.Close()
	s := faviconServer(t)

	if rec := getFavicon(t, s, upstream.URL+"/", nil); rec.Code == http.StatusOK {
		t.Fatalf("want non-2xx before the icon exists, got %d", rec.Code)
	}

	// When the marker ages past its TTL and the site now serves an icon.
	marker := filepath.Join(s.faviconCacheDir, faviconCacheKey(mustHost(t, upstream.URL))+".miss")
	stale := time.Now().Add(-faviconMissTTL - time.Minute)
	if err := os.Chtimes(marker, stale, stale); err != nil {
		t.Fatalf("backdate marker: %v", err)
	}
	hasIcon = true

	// Then the site is resolved again.
	if rec := getFavicon(t, s, upstream.URL+"/", nil); rec.Code != http.StatusOK {
		t.Fatalf("want 200 after the miss expired, got %d", rec.Code)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("a successful resolve must clear the miss marker")
	}
}

func mustHost(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return strings.ToLower(u.Host)
}

func TestHandleFavicon_resolvesFaviconIcoAndServesFromDisk(t *testing.T) {
	// Given a bare site with no icon links and no apple-touch icon, only /favicon.ico.
	var icoHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><head></head><body>hi</body></html>"))
		case "/favicon.ico":
			icoHits++
			writePNG(w)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := faviconServer(t)

	// When the site's page is requested twice (via different paths on the same host).
	first := getFavicon(t, s, upstream.URL+"/some/article", nil)
	second := getFavicon(t, s, upstream.URL+"/another/page", nil)

	// Then both succeed, but the icon is fetched only once (second served from disk,
	// keyed by host regardless of the differing paths).
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
	if icoHits != 1 {
		t.Fatalf("favicon.ico hits = %d, want 1 (second must be cached)", icoHits)
	}
}

func TestHandleFavicon_prefersAppleTouchIconOverFavicon(t *testing.T) {
	// Given a page that declares an apple-touch-icon, plus a fallback /favicon.ico.
	var served string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><head>
				<link rel="icon" href="/favicon.ico">
				<link rel="apple-touch-icon" sizes="180x180" href="/touch.png">
			</head></html>`)
		case "/touch.png":
			served = "apple"
			writePNG(w)
		case "/favicon.ico":
			served = "favicon"
			writePNG(w)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := faviconServer(t)

	rec := getFavicon(t, s, upstream.URL+"/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if served != "apple" {
		t.Fatalf("resolved icon = %q, want the apple-touch-icon", served)
	}
}

func TestHandleFavicon_probesWellKnownAppleTouchWhenUnlinked(t *testing.T) {
	// GitHub's case: the page doesn't link an apple-touch-icon (only an adaptive
	// .svg, which we reject), but one exists at the well-known path.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><head>
				<link rel="icon" type="image/svg+xml" href="/favicon.svg">
			</head></html>`)
		case "/apple-touch-icon.png":
			writePNG(w)
		default:
			http.NotFound(w, r) // no /favicon.ico either
		}
	}))
	defer upstream.Close()
	s := faviconServer(t)

	rec := getFavicon(t, s, upstream.URL+"/torvalds", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 (well-known apple-touch), got %d", rec.Code)
	}
}

func TestHandleFavicon_skipsSVGCandidates(t *testing.T) {
	// The only declared icon is an SVG (rejected); resolution must fall through to
	// /favicon.ico rather than serve the SVG.
	var svgFetched bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprint(w, `<html><head><link rel="icon" href="/icon.svg"></head></html>`)
		case "/icon.svg":
			svgFetched = true
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		case "/favicon.ico":
			writePNG(w)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := faviconServer(t)

	rec := getFavicon(t, s, upstream.URL+"/", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("want 200 image/png (fell through to favicon.ico), got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if svgFetched {
		t.Fatal("SVG candidate must never be fetched")
	}
}

func TestHandleFavicon_returns304OnIfNoneMatch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			writePNG(w)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer upstream.Close()
	s := faviconServer(t)

	first := getFavicon(t, s, upstream.URL+"/", nil)
	etag := first.Header().Get("ETag")

	second := getFavicon(t, s, upstream.URL+"/", http.Header{"If-None-Match": {etag}})
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

func TestHandleFavicon_noUsableIconReturnsError(t *testing.T) {
	// Every candidate 404s (and the service fallback is stubbed empty) → non-2xx, so
	// the frontend renders its letter avatar.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><head></head></html>"))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	s := faviconServer(t)

	rec := getFavicon(t, s, upstream.URL+"/", nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("no usable icon must not return 200")
	}
}

func TestHandleFavicon_rejectsNonImageIcon(t *testing.T) {
	// The favicon.ico serves HTML (a soft-404 page); it must be rejected, not served.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer upstream.Close()
	s := faviconServer(t)

	rec := getFavicon(t, s, upstream.URL+"/", nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("non-image icon must not return 200, got 200")
	}
}

func TestHandleFavicon_setsHardeningHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			writePNG(w)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer upstream.Close()
	s := faviconServer(t)

	rec := getFavicon(t, s, upstream.URL+"/", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing Content-Security-Policy")
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
