package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func getSourceImage(s *server, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/source-image?u="+url.QueryEscape(target), nil)
	rec := httptest.NewRecorder()
	s.handleSourceImage(rec, req)
	return rec
}

func TestHandleSourceImage_proxiesWithoutCachingOnDisk(t *testing.T) {
	var agent string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent = r.Header.Get("User-Agent")
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
	// loom names itself and where to find it: a shop's image host refused
	// the old "loom-favicon-cache/1.0" (403) and serves this one.
	if !strings.HasPrefix(agent, "loom/") || !strings.HasSuffix(agent, " (+https://github.com/trick77/loom)") {
		t.Fatalf("User-Agent = %q", agent)
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
