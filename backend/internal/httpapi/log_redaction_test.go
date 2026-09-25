package httpapi

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

// A share id is the bearer token of a public share; the request log must not
// keep it.
func TestRequestLogRedactsShareToken(t *testing.T) {
	logs := captureLogs(t)
	h := New(Deps{})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/shares/SECRET-SHARE-TOKEN/artifacts/art_1/download", nil))

	if strings.Contains(logs.String(), "SECRET-SHARE-TOKEN") {
		t.Fatalf("request log carries the share token:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "/api/shares/[redacted]/artifacts/art_1/download") {
		t.Fatalf("request log does not show the redacted path:\n%s", logs.String())
	}
}

// serverError logs the underlying cause; a cause that embeds a URL (an upstream
// fetch, a webhook) can carry a key in its query string.
func TestServerErrorRedactsQueryStringAndShareToken(t *testing.T) {
	logs := captureLogs(t)
	req := httptest.NewRequest(http.MethodGet, "/api/shares/SECRET-SHARE-TOKEN", nil)
	req.SetPathValue("shareID", "SECRET-SHARE-TOKEN")
	rec := httptest.NewRecorder()

	serverError(rec, req, errors.New(`fetch https://upstream.example/v1?api_key=SECRET-KEY: boom`), "share failed")

	if strings.Contains(logs.String(), "SECRET-KEY") || strings.Contains(logs.String(), "SECRET-SHARE-TOKEN") {
		t.Fatalf("server error log leaks a secret:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "[redacted]") {
		t.Fatalf("server error log was not redacted:\n%s", logs.String())
	}
}

// A token that also occurs inside an earlier segment is redacted where it is
// the share id, not at its first substring match.
func TestRequestLogRedactsTheShareSegmentNotASubstring(t *testing.T) {
	logs := captureLogs(t)
	h := New(Deps{})
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/shares/api", nil))

	if !strings.Contains(logs.String(), "/api/shares/[redacted]") {
		t.Fatalf("request log does not redact the share segment:\n%s", logs.String())
	}
}
