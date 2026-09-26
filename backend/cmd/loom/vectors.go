package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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

// Retry pacing for a failed re-embed: a rate limit or an upstream outage
// passes, and until the run completes the affected documents are missing from
// retrieval, so it keeps trying instead of waiting for the next restart.
const (
	reembedFirstRetry = time.Minute
	reembedMaxRetry   = 30 * time.Minute
)

// reembedInBackground restores every missing vector without holding up boot.
// It runs on every boot and does nothing when no vector is missing; a failed
// run retries with a growing wait until it completes.
func reembedInBackground(ingester *rag.Ingester) {
	go reembedUntilDone(context.Background(), ingester.ReembedMissing, sleepCtx)
}

// reembedUntilDone runs run until it succeeds, waiting between failures (the
// wait doubles up to reembedMaxRetry). wait returning false ends the loop.
func reembedUntilDone(ctx context.Context, run func(context.Context) (int, error), wait func(context.Context, time.Duration) bool) {
	delay := reembedFirstRetry
	for {
		_, err := run(ctx)
		if err == nil {
			return
		}
		slog.Error("rag: re-embedding failed; retrying", "err", err, "retry_in", delay)
		if !wait(ctx, delay) {
			return
		}
		delay = min(delay*2, reembedMaxRetry)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
