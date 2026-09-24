package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecoveryReturnsJSON500BeforeHeadersWereSent(t *testing.T) {
	h := recovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
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
	h := recovery(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
