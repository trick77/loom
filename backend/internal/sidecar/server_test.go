package sidecar

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	srv := httptest.NewServer(Handler(token, api))
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
			req, _ := http.NewRequest(http.MethodGet, srv.URL+tc.path, nil)
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
}
