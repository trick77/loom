package rag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/trick77/llmwire"
)

// reembedPageSize is how many missing chunks one pass loads; each owner's share
// is embedded in embedBatchSize batches.
const reembedPageSize = 256

// ReembedMissing embeds every stored chunk that has no vector — all of them
// right after RebuildVectorTable, none in the steady state — from the chunk
// text already in the database, and books the usage on each chunk's owner. It
// is safe to run on every boot and to interrupt: what is still missing is
// picked up next time. A chunk the model refuses outright (a bad request, e.g.
// past its input limit) is skipped and logged so it cannot stall the rest; a
// transient failure ends the run with an error so the caller retries. It
// returns how many chunks it embedded.
func (ing *Ingester) ReembedMissing(ctx context.Context) (int, error) {
	total := 0
	var refused []int64
	var after int64
	for {
		missing, err := ing.store.ChunksMissingVectors(ctx, after, reembedPageSize)
		if err != nil {
			return total, err
		}
		if len(missing) == 0 {
			if total == 0 && len(refused) > 0 {
				// Nothing embedded in this run. If the table holds vectors, the
				// model works and these few it will never take (an interrupted
				// run, or chunks an earlier version skipped). If it holds none,
				// the model is refusing every input (a wrong model or
				// parameter): fail so the run is retried, and remember nothing.
				works, err := ing.store.HasVectors(ctx)
				if err != nil {
					return 0, err
				}
				if !works {
					return 0, fmt.Errorf("re-embed: the embedding model refused all %d chunks", len(refused))
				}
			}
			// The model takes other chunks, so these it will never take:
			// remember them, or every later run would fail on them.
			if err := ing.store.MarkRefused(ctx, refused); err != nil {
				return total, err
			}
			if total > 0 || len(refused) > 0 {
				slog.InfoContext(ctx, "rag: re-embedding finished", "chunks", total, "refused", len(refused))
			}
			return total, nil
		}
		after = missing[len(missing)-1].ChunkID
		for _, group := range batches(groupByUser(missing)) {
			done, groupRefused, err := ing.reembedGroup(ctx, group)
			total += done
			refused = append(refused, groupRefused...)
			if err != nil {
				return total, err
			}
		}
		slog.InfoContext(ctx, "rag: re-embedding", "chunks_done", total)
	}
}

// reembedGroup embeds one owner's chunks in a batch. When the model refuses
// the batch as a bad request, it retries chunk by chunk so only the chunks it
// refuses are left out.
func (ing *Ingester) reembedGroup(ctx context.Context, group []MissingVector) (done int, refused []int64, err error) {
	err = ing.embedAndStore(ctx, group)
	switch {
	case err == nil:
		return len(group), nil, nil
	case !errors.Is(err, llmwire.ErrBadRequest):
		return 0, nil, err
	case len(group) == 1:
		slog.WarnContext(ctx, "rag: embedding model refused a chunk; skipped", "chunk_id", group[0].ChunkID, "err", err)
		return 0, []int64{group[0].ChunkID}, nil
	}
	for _, m := range group {
		d, r, err := ing.reembedGroup(ctx, []MissingVector{m})
		done += d
		refused = append(refused, r...)
		if err != nil {
			return done, refused, err
		}
	}
	return done, refused, nil
}

func (ing *Ingester) embedAndStore(ctx context.Context, group []MissingVector) error {
	chunks := make([]TextChunk, len(group))
	for i, m := range group {
		chunks[i] = TextChunk{Text: m.Text}
	}
	vectors, err := ing.embedAll(ctx, group[0].UserID, chunks)
	if err != nil {
		return fmt.Errorf("re-embed chunks: %w", err)
	}
	return ing.store.InsertVectors(ctx, group, vectors)
}

// batches splits each owner's run into embedBatchSize pieces, so every piece is
// one embedding call whose vectors are stored before the next: a refused call
// never discards the calls before it.
func batches(groups [][]MissingVector) [][]MissingVector {
	var out [][]MissingVector
	for _, g := range groups {
		for len(g) > embedBatchSize {
			out = append(out, g[:embedBatchSize])
			g = g[embedBatchSize:]
		}
		out = append(out, g)
	}
	return out
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
