package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSPAHandler_servesIndexFallback(t *testing.T) {
	h := SPAHandler()

	// An unknown client-side route must fall back to index.html (200).
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/123", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("fallback status = %d, want 200", rec.Code)
	}
}
