package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoveryReturnsJSON500BeforeHeadersWereSent(t *testing.T) {
	h := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"internal server error"}` {
		t.Fatalf("body = %s, want JSON error", got)
	}
}

// Once a handler has started its response (an SSE stream, a partial file), a
// trailing error write would corrupt what the client already received; the
// panic is logged and the response is left as it is.
func TestRecoveryDoesNotWriteAfterHeadersWereSent(t *testing.T) {
	h := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		panic("boom")
	}))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want the 200 already sent", rec.Code)
	}
	if rec.Body.String() != "partial" {
		t.Fatalf("body = %q, want the partial response untouched", rec.Body.String())
	}
}

// A share id is a public share's bearer token: no log line carries it, the
// access line included, whose request the mux never set path values on.
func TestAccessLogRedactsShareIDs(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	mux.HandleFunc("GET /api/shares/{shareID}", ok)
	mux.HandleFunc("GET /share/{shareID}", ok)
	h := middleware(mux)

	for _, path := range []string{"/api/shares/tok3n-secret", "/share/tok3n-secret"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if strings.Contains(logs.String(), "tok3n-secret") {
		t.Fatalf("share id logged: %s", logs.String())
	}
	if strings.Count(logs.String(), "[redacted]") != 2 {
		t.Fatalf("want both paths logged redacted: %s", logs.String())
	}
}

func TestLogPath(t *testing.T) {
	for path, want := range map[string]string{
		"/api/shares/abc":                    "/api/shares/[redacted]",
		"/api/shares/abc/artifacts/a1/thumb": "/api/shares/[redacted]/artifacts/a1/thumb",
		"/share/abc":                         "/share/[redacted]",
		"/api/shares":                        "/api/shares",
		"/api/threads/t1/share":              "/api/threads/t1/share",
		"/api/me":                            "/api/me",
	} {
		if got := logPath(httptest.NewRequest(http.MethodGet, path, nil)); got != want {
			t.Errorf("logPath(%s) = %s, want %s", path, got, want)
		}
	}
}
