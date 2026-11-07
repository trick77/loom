package sidecar

import (
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := Handler(token, api)
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, tc := range []struct {
		name, path, auth string
		want             int
	}{
		{"no token", "/mcp", "", http.StatusUnauthorized},
		{"wrong token", "/mcp", "Bearer " + token[1:] + "x", http.StatusUnauthorized},
		{"other scheme", "/mcp", "Basic " + token, http.StatusUnauthorized},
		{"token", "/mcp", "Bearer " + token, http.StatusTeapot},
		{"any route behind the token", "/status", "Bearer " + token, http.StatusTeapot},
		{"health without a token", "/healthz", "", http.StatusOK},
		{"readiness without a token", "/readyz", "", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, srv.URL+tc.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.want)
			}
			if tc.want == http.StatusUnauthorized && resp.Header.Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("WWW-Authenticate %q", resp.Header.Get("WWW-Authenticate"))
			}
		})
	}

	// The probes answer the container's own healthcheck only: a caller across
	// the network gets 404, token or not, never the API's 401.
	for _, auth := range []string{"", "Bearer " + token} {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("healthz from afar (auth %t): %d", auth != "", rec.Code)
		}
	}
}

// Every sidecar image copies only internal/sidecar and internal/loomuser of
// loom (besides its own packages), and CI builds no image: an import of any
// other loom package here would pass make check and break every sidecar's
// build at release.
func TestImportsNoOtherLoomPackage(t *testing.T) {
	const loom = "github.com/trick77/loom/"
	allowed := map[string]map[string]bool{
		".":           {loom + "internal/loomuser": true},
		"../loomuser": {},
	}
	for dir, ok := range allowed {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: no Go files (%v)", dir, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if strings.HasPrefix(path, loom) && !ok[path] {
					t.Errorf("%s imports %s: the sidecar images do not copy it", file, path)
				}
			}
		}
	}
}
