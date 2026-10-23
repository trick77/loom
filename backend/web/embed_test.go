package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func serve(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandler_unknownRouteGetsTheShell(t *testing.T) {
	h := handler(fstest.MapFS{"index.html": {Data: []byte("INDEX")}})
	rec := serve(h, "/projects/123")
	if rec.Code != http.StatusOK || rec.Body.String() != "INDEX" {
		t.Errorf("/projects/123 = %d %q, want 200 INDEX", rec.Code, rec.Body.String())
	}
	// A mistyped endpoint stays a 404, not HTML posing as JSON.
	if rec := serve(h, "/api/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("/api/nope = %d, want 404", rec.Code)
	}
}

func TestHandler_unbuiltServesPlaceholder(t *testing.T) {
	rec := serve(handler(fstest.MapFS{".gitkeep": {}}), "/")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "built without its UI") {
		t.Errorf("unbuilt / = %d %q, want 503 and the placeholder", rec.Code, rec.Body.String())
	}
}

func TestHandler_retiredIconPathsReturn404(t *testing.T) {
	for path := range retiredIcons {
		rec := serve(handler(fstest.MapFS{"index.html": {Data: []byte("INDEX")}}), path)
		if rec.Code != http.StatusNotFound || rec.Body.String() == "INDEX" {
			t.Errorf("%s = %d %q, want a 404", path, rec.Code, rec.Body.String())
		}
		// The guard must not shadow a real file: a restored icon serves normally.
		withIcon := handler(fstest.MapFS{
			"index.html":                  {Data: []byte("INDEX")},
			strings.TrimPrefix(path, "/"): {Data: []byte("ICON")},
		})
		if rec := serve(withIcon, path); rec.Code != http.StatusOK || rec.Body.String() != "ICON" {
			t.Errorf("%s with file present = %d %q, want 200 ICON", path, rec.Code, rec.Body.String())
		}
	}
}

func TestHandler_robotsAndShareNoindex(t *testing.T) {
	h := handler(fstest.MapFS{"index.html": {Data: []byte("INDEX")}})
	if rec := serve(h, "/robots.txt"); !strings.Contains(rec.Body.String(), "Disallow: /share/") {
		t.Errorf("robots.txt = %q", rec.Body.String())
	}
	if rec := serve(h, "/share/abc"); !strings.Contains(rec.Header().Get("X-Robots-Tag"), "noindex") {
		t.Errorf("/share/ X-Robots-Tag = %q, want noindex", rec.Header().Get("X-Robots-Tag"))
	}
}

func TestIndexHTML_tracksTheBuild(t *testing.T) {
	_, ok := IndexHTML()
	if _, err := fs.Stat(dist(), "index.html"); (err == nil) != ok {
		t.Errorf("IndexHTML ok = %v, disagrees with the embedded dist", ok)
	}
}
