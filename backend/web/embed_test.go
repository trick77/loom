package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPAHandler_servesIndexFallback(t *testing.T) {
	h := SPAHandler()

	// An unknown client-side route must fall back to index.html (200).
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/123", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("fallback status = %d, want 200", rec.Code)
	}
}

func TestSpaHandler_retiredIconPathsReturn404(t *testing.T) {
	// The icon paths loom no longer serves: /favicon.ico is probed blindly by
	// unfurlers and feed readers, /favicon.svg was the master before it was
	// renamed to /icon.svg, and the manifest PNGs are held by already-installed
	// PWAs. None of them parse HTML first, so a 200 of markup is worse than a
	// clean miss.
	paths := []string{
		"/favicon.ico",
		"/favicon.svg",
		"/web-app-manifest-192x192.png",
		"/web-app-manifest-512x512.png",
	}

	h := spaHandler(fstest.MapFS{"index.html": {Data: []byte("INDEX")}})
	for _, path := range paths {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, rec.Code)
		}
		if rec.Body.String() == "INDEX" {
			t.Errorf("%s served index.html, want 404 body", path)
		}
	}

	// The guard must not shadow a real file: if an icon is ever restored to
	// ui/public, it is served normally rather than permanently 404ing.
	for _, path := range paths {
		name := trimLeadingSlash(path)
		withIcon := spaHandler(fstest.MapFS{
			"index.html": {Data: []byte("INDEX")},
			name:         {Data: []byte("ICON")},
		})
		rec := httptest.NewRecorder()
		withIcon.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "ICON" {
			t.Errorf("%s with file present = %d %q, want 200 ICON", path, rec.Code, rec.Body.String())
		}
	}
}

func TestSpaHandler_directoryPathFallsBackToIndex(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("INDEX")},
		"assets/app.js": {Data: []byte("APP")},
	}
	h := spaHandler(fsys)

	// A directory path must fall back to index.html, NOT render a listing.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/assets status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "INDEX" {
		t.Errorf("/assets body = %q, want INDEX (index fallback, no dir listing)", rec.Body.String())
	}

	// An existing regular file is served directly.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rec2.Body.String() != "APP" {
		t.Errorf("/assets/app.js body = %q, want APP", rec2.Body.String())
	}

	// An unknown client-side route falls back to index.html.
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/projects/123", nil))
	if rec3.Body.String() != "INDEX" {
		t.Errorf("/projects/123 body = %q, want INDEX", rec3.Body.String())
	}
}
