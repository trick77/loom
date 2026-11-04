package sidecar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/loomuser"
)

func TestClientDo(t *testing.T) {
	var lastUser, lastAuth, lastType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		lastUser, lastAuth, lastType = r.Header.Get(loomuser.Header), r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		switch r.URL.Path {
		case "/ok":
			_, _ = fmt.Fprintf(w, `{"echo":%q}`, string(b))
		case "/bad":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprint(w, `{"error":"nope","code":"malformed"}`)
		case "/odd422":
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = fmt.Fprint(w, `not json`)
		case "/gw":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = fmt.Fprint(w, `{"error":"x","code":"bridge"}`)
		case "/disk":
			w.WriteHeader(http.StatusInternalServerError)
		case "/auth":
			w.WriteHeader(http.StatusUnauthorized)
		case "/garbage":
			_, _ = fmt.Fprint(w, `{`)
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer srv.Close()
	c := New("thing", srv.URL+"/", "tok", "BACKEND_THING_TOKEN",
		Codes{Unprocessable: []string{"rejected", "malformed"}, BadGateway: []string{"upstream", "bridge"}}, time.Second)
	ctx := context.Background()

	var out struct{ Echo string }
	if err := c.Do(ctx, "u1", http.MethodPut, "/ok", []byte(`{"a":1}`), &out); err != nil || out.Echo != `{"a":1}` {
		t.Fatalf("ok: %v %+v", err, out)
	}
	if lastUser != "u1" || lastAuth != "Bearer tok" || lastType != "application/json" {
		t.Errorf("headers: %q %q %q", lastUser, lastAuth, lastType)
	}
	for path, want := range map[string]string{"/bad": "malformed", "/odd422": "rejected", "/gw": "bridge", "/disk": CodeStorage, "/auth": CodeConfig} {
		var e *Error
		err := c.Do(ctx, "u1", http.MethodGet, path, nil, &out)
		if !errors.As(err, &e) || e.Code != want || e.Name != "thing" {
			t.Errorf("%s: %v, want %s", path, err, want)
		}
		unavailable := want == CodeStorage || want == CodeConfig
		if errors.Is(err, ErrUnavailable) != unavailable {
			t.Errorf("%s: unavailable = %v", path, !unavailable)
		}
	}
	var e *Error
	if err := c.Do(ctx, "u1", http.MethodGet, "/auth", nil, &out); !errors.As(err, &e) || !strings.Contains(e.Reason, "BACKEND_THING_TOKEN") || !strings.HasPrefix(err.Error(), "thing: config:") {
		t.Errorf("config: %v", err)
	}
	for _, path := range []string{"/garbage", "/teapot?x=1"} {
		if err := c.Do(ctx, "u1", http.MethodGet, path, nil, &out); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "x=1") {
			t.Errorf("%s: %v", path, err)
		}
	}
	lastUser = "untouched"
	if err := c.Do(ctx, "", http.MethodGet, "/ok", nil, &out); !errors.Is(err, ErrUnavailable) || lastUser != "untouched" {
		t.Errorf("no user reached the sidecar: %v", err)
	}
	srv.Close()
	if err := c.Do(ctx, "u1", http.MethodGet, "/ok", nil, &out); !errors.Is(err, ErrUnavailable) {
		t.Errorf("down: %v", err)
	}
	if codeOf(nil, nil) != "" {
		t.Error("codeOf without codes")
	}
}
