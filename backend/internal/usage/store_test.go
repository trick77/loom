package usage_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/trick77/loom/internal/store"
	"github.com/trick77/loom/internal/usage"
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

func TestCounters_surviveChatDeletion(t *testing.T) {
	st, db := newTestStore(t)
	ctx := context.Background()

	// Seed a chat (thread) + message owned by the user, then count some usage.
	if _, err := db.Exec(`INSERT INTO threads (id, user_id, title) VALUES (?, ?, ?)`, "t1", "u1", "Chat"); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO messages (id, thread_id, user_id, role, content) VALUES (?, ?, ?, 'user', 'hi')`,
		"m1", "t1", "u1",
	); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := st.AddTokens(ctx, "u1", usage.TokenDelta{TotalTokens: 100}); err != nil {
		t.Fatalf("AddTokens: %v", err)
	}
	if err := st.IncWebSearch(ctx, "u1"); err != nil {
		t.Fatalf("IncWebSearch: %v", err)
	}

	// Deleting the chat (cascades to its messages) must not touch the counters.
	if _, err := db.Exec(`DELETE FROM threads WHERE id = ?`, "t1"); err != nil {
		t.Fatalf("delete thread: %v", err)
	}

	got, err := st.Get(ctx, "u1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TotalTokens != 100 || got.WebSearches != 1 {
		t.Fatalf("counters changed after chat deletion: %+v", got)
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
	if err := st.AddEmbeddingUsage(ctx, "u1", 11, 1); err != nil {
		t.Fatalf("AddEmbeddingUsage: %v", err)
	}
	if err := st.AddEmbeddingUsage(ctx, "u1", 5, 2); err != nil {
		t.Fatalf("AddEmbeddingUsage 2: %v", err)
	}
	for _, inc := range []func(context.Context, string) error{
		st.IncWebSearch, st.IncWebFetch, st.IncObscuraFetch,
		st.IncImageGen, st.IncThreadCreated, st.IncProjectCreated,
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
		EmbeddingTokens: 16, EmbeddingRequests: 3,
		WebSearches: 1, WebFetches: 1, ObscuraFetches: 1, ImageGens: 1, ThreadsCreated: 1, ProjectsCreated: 1,
	}
	if got != want {
		t.Fatalf("totals mismatch:\n got %+v\nwant %+v", got, want)
	}
}
