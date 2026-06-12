package usage_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/trick77/slopr/internal/store"
	"github.com/trick77/slopr/internal/usage"
)

// newTestStore opens a real migrated database (so the test exercises migration
// 0005 and the foreign key to users) and seeds one user the counters can target.
func newTestStore(t *testing.T) (*usage.Store, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "usage_test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(
		`INSERT INTO users (id, oidc_subject, username, role) VALUES (?, ?, ?, 'user')`,
		"u1", "subject-u1", "u1",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return usage.NewStore(db), db
}

func TestGet_unknownUser_returnsZeroTotals(t *testing.T) {
	store, _ := newTestStore(t)
	got, err := store.Get(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != (usage.Totals{}) {
		t.Fatalf("want zero Totals, got %+v", got)
	}
}

func TestCounters_areAdditiveAndCreateRow(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	if err := st.AddTokens(ctx, "u1", usage.TokenDelta{PromptTokens: 10, CompletionTokens: 5, CachedTokens: 2, ReasoningTokens: 3, TotalTokens: 18}); err != nil {
		t.Fatalf("AddTokens: %v", err)
	}
	if err := st.AddTokens(ctx, "u1", usage.TokenDelta{PromptTokens: 1, TotalTokens: 1}); err != nil {
		t.Fatalf("AddTokens 2: %v", err)
	}
	for _, inc := range []func(context.Context, string) error{
		st.IncWebSearch, st.IncWebFetch, st.IncObscuraFetch,
		st.IncImageGen, st.IncChatCreated, st.IncProjectCreated,
	} {
		if err := inc(ctx, "u1"); err != nil {
			t.Fatalf("inc: %v", err)
		}
	}
	got, err := st.Get(ctx, "u1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := usage.Totals{
		PromptTokens: 11, CompletionTokens: 5, CachedTokens: 2, ReasoningTokens: 3, TotalTokens: 19,
		WebSearches: 1, WebFetches: 1, ObscuraFetches: 1, ImageGens: 1, ChatsCreated: 1, ProjectsCreated: 1,
	}
	if got != want {
		t.Fatalf("totals mismatch:\n got %+v\nwant %+v", got, want)
	}
}
