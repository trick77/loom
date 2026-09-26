package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/trick77/loom/internal/rag"
)

// reconcileVectorWidth rebuilds the vector table when the configured embedding
// model's width differs from the table's, which is what a change of
// BACKEND_EMBED_MODEL means. Every vector is dropped and re-embedded from the
// stored chunk text (see reembedInBackground); until then retrieval finds
// nothing for those documents.
func reconcileVectorWidth(ctx context.Context, store *rag.Store, model rag.EmbedModel) error {
	width, err := store.VectorWidth(ctx)
	if err != nil {
		return err
	}
	if width == model.Width {
		return nil
	}
	slog.Warn("embedding model width changed; rebuilding vectors and re-embedding every document",
		"model", model.ID, "from", width, "to", model.Width)
	if err := store.RebuildVectorTable(ctx, model.Width); err != nil {
		return fmt.Errorf("rebuild vector table: %w", err)
	}
	return nil
}

// reembedInBackground restores every missing vector without holding up boot.
// It runs on every boot and does nothing when no vector is missing; an
// interrupted run resumes on the next boot.
func reembedInBackground(ingester *rag.Ingester) {
	go func() {
		if _, err := ingester.ReembedMissing(context.Background()); err != nil {
			slog.Error("rag: re-embedding failed; retried on the next boot", "err", err)
		}
	}()
}
