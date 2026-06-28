package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/usage"
)

type stubUsageStore struct{ totals usage.Totals }

func (s stubUsageStore) AddTokens(context.Context, string, usage.TokenDelta) error { return nil }
func (s stubUsageStore) AddEmbeddingUsage(context.Context, string, int, int) error { return nil }
func (s stubUsageStore) IncWebSearch(context.Context, string) error                { return nil }
func (s stubUsageStore) IncWebFetch(context.Context, string) error                 { return nil }
func (s stubUsageStore) IncObscuraFetch(context.Context, string) error             { return nil }
func (s stubUsageStore) IncImageGen(context.Context, string) error                 { return nil }
func (s stubUsageStore) IncThreadCreated(context.Context, string) error            { return nil }
func (s stubUsageStore) IncProjectCreated(context.Context, string) error           { return nil }
func (s stubUsageStore) Get(context.Context, string) (usage.Totals, error)         { return s.totals, nil }

func TestHandleGetUsage_returnsTotalsAndMemoryStats(t *testing.T) {
	updated := time.Date(2026, 6, 26, 8, 30, 0, 0, time.UTC)
	threadStore := &fakeThreadStore{
		userMemory:       chat.UserMemory{Content: "hello", SourceMessageCount: 184, UpdatedAt: &updated},
		userMessageCount: 210,
	}
	srv := newAuthenticatedServer(t, Deps{
		Thread: threadStore,
		Usage:  stubUsageStore{totals: usage.Totals{TotalTokens: 42, EmbeddingTokens: 12, EmbeddingRequests: 2, WebSearches: 3}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodGet, "/api/me/usage", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got usageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TotalTokens != 42 || got.WebSearches != 3 {
		t.Fatalf("totals not surfaced: %+v", got)
	}
	if got.EmbeddingTokens != 12 || got.EmbeddingRequests != 2 {
		t.Fatalf("embedding usage not surfaced: %+v", got)
	}
	if got.UserMemoryLength != 5 {
		t.Fatalf("UserMemoryLength = %d, want 5", got.UserMemoryLength)
	}
	if got.UserMemoryMax != chat.MaxUserMemoryLength {
		t.Fatalf("UserMemoryMax = %d, want %d", got.UserMemoryMax, chat.MaxUserMemoryLength)
	}
	if got.UserMemorySourceMessages != 184 {
		t.Fatalf("UserMemorySourceMessages = %d, want 184", got.UserMemorySourceMessages)
	}
	if got.UserMemoryTotalMessages != 210 {
		t.Fatalf("UserMemoryTotalMessages = %d, want 210", got.UserMemoryTotalMessages)
	}
	if got.UserMemoryUpdatedAt == nil || *got.UserMemoryUpdatedAt != "2026-06-26T08:30:00Z" {
		t.Fatalf("UserMemoryUpdatedAt = %v, want 2026-06-26T08:30:00Z", got.UserMemoryUpdatedAt)
	}
	if got.UserMemoryRefreshWindowHours != 24 {
		t.Fatalf("UserMemoryRefreshWindowHours = %d, want 24", got.UserMemoryRefreshWindowHours)
	}
}

// TestHandleGetUsage_neverGeneratedMemory proves a user with no memory yet reports
// zeroed memory stats and a null updated-at (not a zero timestamp).
func TestHandleGetUsage_neverGeneratedMemory(t *testing.T) {
	threadStore := &fakeThreadStore{userMessageCount: 7} // no userMemory → GetUserMemory ok=false
	srv := newAuthenticatedServer(t, Deps{Thread: threadStore, Usage: stubUsageStore{}})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodGet, "/api/me/usage", "")

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got usageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.UserMemoryUpdatedAt != nil {
		t.Fatalf("UserMemoryUpdatedAt = %v, want nil for never-generated memory", *got.UserMemoryUpdatedAt)
	}
	if got.UserMemoryLength != 0 || got.UserMemorySourceMessages != 0 {
		t.Fatalf("memory length/source = %d/%d, want 0/0", got.UserMemoryLength, got.UserMemorySourceMessages)
	}
	if got.UserMemoryTotalMessages != 7 {
		t.Fatalf("UserMemoryTotalMessages = %d, want 7", got.UserMemoryTotalMessages)
	}
}
