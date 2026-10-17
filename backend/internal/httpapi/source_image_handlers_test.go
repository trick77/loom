package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func getSourceImage(s *server, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/source-image?u="+url.QueryEscape(target), nil)
	rec := httptest.NewRecorder()
	s.handleSourceImage(rec, req)
	return rec
}

func TestHandleSourceImage_proxiesAndCachesAnImage(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writePNG(w)
	}))
	defer upstream.Close()
	s := &server{faviconCacheDir: t.TempDir(), faviconClient: upstream.Client()}

	for i := 0; i < 2; i++ {
		rec := getSourceImage(s, upstream.URL+"/vi/abc/mqdefault.jpg")
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("request %d: status %d, type %q", i, rec.Code, rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("request %d: missing nosniff", i)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("upstream hits = %d, want 1 (second from cache)", hits.Load())
	}
}

func TestHandleSourceImage_rejectsNonImagesAndNonHTTPS(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<script>alert(1)</script>"))
	}))
	defer upstream.Close()
	s := &server{faviconCacheDir: t.TempDir(), faviconClient: upstream.Client()}

	if rec := getSourceImage(s, upstream.URL+"/page"); rec.Code == http.StatusOK {
		t.Fatal("a non-image must not be served")
	}
	for _, bad := range []string{"", "http://example.com/a.jpg", "javascript:alert(1)", "/relative.jpg"} {
		if rec := getSourceImage(s, bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: status %d, want 400", bad, rec.Code)
		}
	}
}
