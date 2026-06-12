package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/usage"
)

type stubUsageStore struct{ totals usage.Totals }

func (s stubUsageStore) AddTokens(context.Context, string, usage.TokenDelta) error { return nil }
func (s stubUsageStore) AddEmbeddingUsage(context.Context, string, int, int) error { return nil }
func (s stubUsageStore) IncWebSearch(context.Context, string) error                { return nil }
func (s stubUsageStore) IncWebFetch(context.Context, string) error                 { return nil }
func (s stubUsageStore) IncObscuraFetch(context.Context, string) error             { return nil }
func (s stubUsageStore) IncImageGen(context.Context, string) error                 { return nil }
func (s stubUsageStore) IncChatCreated(context.Context, string) error              { return nil }
func (s stubUsageStore) IncProjectCreated(context.Context, string) error           { return nil }
func (s stubUsageStore) Get(context.Context, string) (usage.Totals, error)         { return s.totals, nil }

func TestHandleGetUsage_returnsTotalsAndMemoryLength(t *testing.T) {
	chatStore := &fakeChatStore{userMemory: chat.UserMemory{Content: "hello"}}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat:  chatStore,
		Usage: stubUsageStore{totals: usage.Totals{TotalTokens: 42, EmbeddingTokens: 12, EmbeddingRequests: 2, WebSearches: 3}},
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
}
