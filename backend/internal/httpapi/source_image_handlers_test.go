package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
)

func getSourceImage(s *server, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/source-image?u="+url.QueryEscape(target), nil)
	rec := httptest.NewRecorder()
	s.handleSourceImage(rec, req)
	return rec
}

func TestHandleSourceImage_proxiesWithoutCachingOnDisk(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writePNG(w)
	}))
	defer upstream.Close()
	cacheDir := t.TempDir()
	s := &server{faviconCacheDir: cacheDir, faviconClient: upstream.Client()}

	rec := getSourceImage(s, upstream.URL+"/vi/abc/mqdefault.jpg?n=1")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Cache-Control") != sourceImageCacheControl {
		t.Fatalf("headers = %v", rec.Header())
	}
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 0 {
		t.Fatalf("source images must not be written to disk, found %d entries", len(entries))
	}
}

func TestHandleSourceImage_failureIsCachedOnlyBriefly(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<script>alert(1)</script>"))
	}))
	defer upstream.Close()
	s := &server{faviconClient: upstream.Client()}

	rec := getSourceImage(s, upstream.URL+"/page")
	if rec.Code == http.StatusOK || rec.Header().Get("Cache-Control") != sourceImageMissCacheControl {
		t.Fatalf("status %d, cache %q; want a briefly cached failure", rec.Code, rec.Header().Get("Cache-Control"))
	}
	for _, bad := range []string{"", "http://example.com/a.jpg", "javascript:alert(1)", "/relative.jpg"} {
		if rec := getSourceImage(s, bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: status %d, want 400", bad, rec.Code)
		}
	}
}
