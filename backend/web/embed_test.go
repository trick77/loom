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

func TestSpaHandler_faviconICOReturns404(t *testing.T) {
	fsys := fstest.MapFS{"index.html": {Data: []byte("INDEX")}}
	h := spaHandler(fsys)

	// /favicon.ico must 404, not fall through to index.html: unfurlers and feed
	// readers probe it without parsing HTML, so a 200 of markup is worse than a
	// clean miss.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("/favicon.ico status = %d, want 404", rec.Code)
	}
	if rec.Body.String() == "INDEX" {
		t.Error("/favicon.ico served index.html, want 404 body")
	}

	// The guard must not shadow a real file: if an icon is ever restored to
	// ui/public, it is served normally rather than permanently 404ing.
	withICO := spaHandler(fstest.MapFS{
		"index.html":  {Data: []byte("INDEX")},
		"favicon.ico": {Data: []byte("ICO")},
	})
	rec2 := httptest.NewRecorder()
	withICO.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec2.Code != http.StatusOK || rec2.Body.String() != "ICO" {
		t.Errorf("/favicon.ico with file present = %d %q, want 200 ICO", rec2.Code, rec2.Body.String())
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
