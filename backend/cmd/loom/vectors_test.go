package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/store"
)

func TestReconcileVectorWidth(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := rag.NewStore(db)
	ctx := context.Background()
	width, err := s.VectorWidth(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Same width: nothing to do.
	if err := reconcileVectorWidth(ctx, s, rag.EmbedModel{ID: "m", Width: width}); err != nil {
		t.Fatalf("same width: %v", err)
	}
	if got, _ := s.VectorWidth(ctx); got != width {
		t.Fatalf("same width changed the table to %d", got)
	}

	// Another model's width: the table follows it.
	if err := reconcileVectorWidth(ctx, s, rag.EmbedModel{ID: "m2", Width: 8}); err != nil {
		t.Fatalf("new width: %v", err)
	}
	if got, _ := s.VectorWidth(ctx); got != 8 {
		t.Fatalf("width = %d, want 8", got)
	}
}

// Two embedding models with the same width still write incompatible vectors:
// a model change rebuilds the table even when the width matches.
func TestReconcileVectorWidth_SameWidthModelChangeRebuilds(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := rag.NewStore(db)
	ctx := context.Background()
	if err := reconcileVectorWidth(ctx, s, rag.EmbedModel{ID: "m1", Width: 8}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u1','s1','u1','user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id) VALUES (1, '[1,0,0,0,0,0,0,0]', 'u1', '')`); err != nil {
		t.Fatal(err)
	}

	// Same model again: vectors kept.
	if err := reconcileVectorWidth(ctx, s, rag.EmbedModel{ID: "m1", Width: 8}); err != nil {
		t.Fatal(err)
	}
	if n := vectorCount(t, db); n != 1 {
		t.Fatalf("same model dropped vectors: %d left", n)
	}
	// Another model, same width: vectors dropped for re-embedding.
	if err := reconcileVectorWidth(ctx, s, rag.EmbedModel{ID: "m2", Width: 8}); err != nil {
		t.Fatal(err)
	}
	if n := vectorCount(t, db); n != 0 {
		t.Fatalf("model change kept %d vectors from the old model", n)
	}
	if got, _, _ := s.VectorModel(ctx); got != "m2" {
		t.Fatalf("recorded model = %q, want m2", got)
	}
}

// A failed re-embed (a 429, a timeout) retries after a growing wait instead
// of leaving documents out of retrieval until the next restart.
func TestReembedUntilDone_RetriesWithBackoff(t *testing.T) {
	var waits []time.Duration
	calls := 0
	run := func(context.Context) (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("rate limited")
		}
		return 5, nil
	}
	reembedUntilDone(context.Background(), run, func(_ context.Context, d time.Duration) bool {
		waits = append(waits, d)
		return true
	})
	if calls != 3 {
		t.Fatalf("runs = %d, want 3", calls)
	}
	if len(waits) != 2 || waits[1] <= waits[0] {
		t.Fatalf("waits = %v, want two growing waits", waits)
	}
}

// Shutdown ends the retry loop.
func TestReembedUntilDone_StopsWhenTheWaitIsCancelled(t *testing.T) {
	calls := 0
	reembedUntilDone(context.Background(), func(context.Context) (int, error) {
		calls++
		return 0, errors.New("down")
	}, func(context.Context, time.Duration) bool { return false })
	if calls != 1 {
		t.Fatalf("runs = %d, want 1", calls)
	}
}

// A database from before the model was recorded holds vectors from an unknown
// model: re-embed once rather than trust them to match the configured one.
func TestReconcileVectorWidth_UnrecordedVectorsAreReembedded(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "loom.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := rag.NewStore(db)
	ctx := context.Background()
	if err := s.RebuildVectorTable(ctx, 8); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u1','s1','u1','user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id) VALUES (1, '[1,0,0,0,0,0,0,0]', 'u1', '')`); err != nil {
		t.Fatal(err)
	}

	if err := reconcileVectorWidth(ctx, s, rag.EmbedModel{ID: "m1", Width: 8}); err != nil {
		t.Fatal(err)
	}
	if n := vectorCount(t, db); n != 0 {
		t.Fatalf("kept %d vectors of an unknown model", n)
	}
	if got, _, _ := s.VectorModel(ctx); got != "m1" {
		t.Fatalf("recorded model = %q, want m1", got)
	}
}

func vectorCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM vec_chunks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
