package rag

import (
	"context"
	"fmt"
	"log/slog"
)

// reembedPageSize is how many missing chunks one pass loads; each owner's share
// is embedded in embedBatchSize batches.
const reembedPageSize = 256

// ReembedMissing embeds every stored chunk that has no vector — all of them
// right after RebuildVectorTable, none in the steady state — from the chunk
// text already in the database, and books the usage on each chunk's owner. It
// is safe to run on every boot and to interrupt: what is still missing is
// picked up next time. It returns how many chunks it embedded.
func (ing *Ingester) ReembedMissing(ctx context.Context) (int, error) {
	total := 0
	for {
		missing, err := ing.store.ChunksMissingVectors(ctx, reembedPageSize)
		if err != nil {
			return total, err
		}
		if len(missing) == 0 {
			if total > 0 {
				slog.InfoContext(ctx, "rag: re-embedding finished", "chunks", total)
			}
			return total, nil
		}
		for _, group := range groupByUser(missing) {
			chunks := make([]TextChunk, len(group))
			for i, m := range group {
				chunks[i] = TextChunk{Text: m.Text}
			}
			vectors, err := ing.embedAll(ctx, group[0].UserID, chunks)
			if err != nil {
				return total, fmt.Errorf("re-embed chunks: %w", err)
			}
			if err := ing.store.InsertVectors(ctx, group, vectors); err != nil {
				return total, err
			}
			total += len(group)
		}
		slog.InfoContext(ctx, "rag: re-embedding", "chunks_done", total)
	}
}

// groupByUser splits missing chunks into runs of one owner, keeping order, so
// each owner's embedding usage is booked on them.
func groupByUser(missing []MissingVector) [][]MissingVector {
	var groups [][]MissingVector
	for _, m := range missing {
		if n := len(groups); n > 0 && groups[n-1][0].UserID == m.UserID {
			groups[n-1] = append(groups[n-1], m)
			continue
		}
		groups = append(groups, []MissingVector{m})
	}
	return groups
}
