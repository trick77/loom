package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/trick77/loom/internal/rag"
)

// reconcileVectorWidth rebuilds the vector table when the configured
// embedding model is not the one that wrote it: a different width, or a
// different model of the same width (its vectors live in another space). Every
// vector is dropped and re-embedded from the stored chunk text (see
// reembedInBackground); until then retrieval finds nothing for those
// documents. A database without a recorded model (from before the record
// existed) adopts the configured one.
func reconcileVectorWidth(ctx context.Context, store *rag.Store, model rag.EmbedModel) error {
	width, err := store.VectorWidth(ctx)
	if err != nil {
		return err
	}
	recorded, known, err := store.VectorModel(ctx)
	if err != nil {
		return err
	}
	switch {
	case width != model.Width:
		slog.Warn("embedding model width changed; rebuilding vectors and re-embedding every document",
			"model", model.ID, "from", width, "to", model.Width)
	case known && recorded != model.ID:
		slog.Warn("embedding model changed; rebuilding vectors and re-embedding every document",
			"model", model.ID, "from", recorded)
	default:
		return store.SetVectorModel(ctx, model.ID)
	}
	if err := store.RebuildVectorTable(ctx, model.Width); err != nil {
		return fmt.Errorf("rebuild vector table: %w", err)
	}
	return store.SetVectorModel(ctx, model.ID)
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
