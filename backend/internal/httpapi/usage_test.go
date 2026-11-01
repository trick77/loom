package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/usage"
)

func TestHandleGetUsage_returnsTotalsAndMemoryStats(t *testing.T) {
	updated := time.Date(2026, 6, 26, 8, 30, 0, 0, time.UTC)
	threadStore := &fakeThreadStore{
		Facts: []chat.UserFact{
			fact("a", "work", "Backend dev", true, updated),
			fact("b", "interest", "Climbs", false, updated),
		},
		FactCursor: &chat.UserFactCursor{LastRefreshAt: &updated},
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: threadStore,
		Usage:  stubUsageStore{Totals: usage.Totals{TotalTokens: 42, EmbeddingTokens: 12, EmbeddingRequests: 2, WebSearches: 3}},
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/me/usage", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got usageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TotalTokens != 42 || got.WebSearches != 3 || got.EmbeddingTokens != 12 || got.EmbeddingRequests != 2 {
		t.Fatalf("totals not surfaced: %+v", got)
	}
	if got.UserFactsCount != 2 || got.UserFactsLongTerm != 1 {
		t.Fatalf("facts = %d/%d long-term, want 2/1", got.UserFactsCount, got.UserFactsLongTerm)
	}
	if got.UserMemoryUpdatedAt == nil || *got.UserMemoryUpdatedAt != "2026-06-26T08:30:00Z" {
		t.Fatalf("UserMemoryUpdatedAt = %v, want 2026-06-26T08:30:00Z", got.UserMemoryUpdatedAt)
	}
	if got.UserMemoryRefreshWindowHours != 24 {
		t.Fatalf("UserMemoryRefreshWindowHours = %d, want 24", got.UserMemoryRefreshWindowHours)
	}
}

// TestHandleGetUsage_neverRefreshed proves a user with no refresh yet reports
// zero facts and a null updated-at (not a zero timestamp).
func TestHandleGetUsage_neverRefreshed(t *testing.T) {
	srv := newAuthenticatedServer(t, Deps{Thread: &fakeThreadStore{}, Usage: stubUsageStore{}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/me/usage", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got usageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UserMemoryUpdatedAt != nil || got.UserFactsCount != 0 {
		t.Fatalf("got %+v, want no facts and a nil updated-at", got)
	}
}
