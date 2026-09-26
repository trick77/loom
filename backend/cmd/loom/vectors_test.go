package main

import (
	"context"
	"path/filepath"
	"testing"

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
