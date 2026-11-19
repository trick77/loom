package sidecar

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDispatch(t *testing.T) {
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer health.Close()
	addr := strings.TrimPrefix(health.URL, "http://")
	getenv := func(k string) string {
		if k == "THING_ADDR" {
			return " " + addr + " "
		}
		return ""
	}
	served := 0
	serve := func() error { served++; return nil }
	failing := func() error { return errors.New("boom") }

	for name, c := range map[string]struct {
		args   []string
		serve  func() error
		code   int
		stderr string
	}{
		"no command":   {nil, serve, 2, "usage: loom-thing serve|healthcheck\n"},
		"unknown":      {[]string{"run"}, serve, 1, "loom-thing: unknown command \"run\"\n"},
		"serve":        {[]string{"serve"}, serve, 0, ""},
		"serve failed": {[]string{"serve"}, failing, 1, "loom-thing: boom\n"},
		"healthcheck":  {[]string{"healthcheck"}, serve, 0, ""},
	} {
		var stderr strings.Builder
		if code := dispatch("loom-thing", "THING_ADDR", ":1", c.args, getenv, &stderr, c.serve); code != c.code || stderr.String() != c.stderr {
			t.Errorf("%s: %d %q, want %d %q", name, code, stderr.String(), c.code, c.stderr)
		}
	}
	if served != 1 {
		t.Errorf("served %d times", served)
	}
	// Unset falls back to the default address: nothing listens there.
	var stderr strings.Builder
	if code := dispatch("loom-thing", "OTHER_ADDR", "127.0.0.1:1", []string{"healthcheck"}, getenv, &stderr, serve); code != 1 {
		t.Errorf("healthcheck on the default: %d", code)
	}
}

// A root-owned host dir (Docker creates a missing bind mount so) fails the
// start with the fix in the message; a writable one leaves nothing behind.
func TestWritable(t *testing.T) {
	dir := t.TempDir()
	if err := Writable(dir); err != nil {
		t.Fatal(err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("the check left %v", left)
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := Writable(ro); err == nil || !strings.Contains(err.Error(), "chown 65532:65532") {
		t.Errorf("read-only dir: %v", err)
	}
}
