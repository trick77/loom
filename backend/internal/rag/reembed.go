package rag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/trick77/llmwire"
	"github.com/trick77/loom/internal/inference"
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
	var refused []MissingVector
	var after int64
	for {
		missing, err := ing.store.ChunksMissingVectors(ctx, after, reembedPageSize)
		if err != nil {
			return total, err
		}
		if len(missing) == 0 {
			done, confirmed, err := ing.confirmRefusals(ctx, refused)
			total += done
			if err != nil {
				return total, err
			}
			if total > 0 || len(confirmed) > 0 {
				slog.InfoContext(ctx, "rag: re-embedding finished", "chunks", total, "refused", len(confirmed))
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

// confirmRefusals decides which refused chunks the model will never take. A
// bad request is also how a retired model, a billing error or a rejected
// parameter answers, and then every input fails and nothing is wrong with the
// chunks. So the model is probed first: if it fails, the run fails, nothing is
// recorded and it is retried. If it answers, each refused chunk is tried once
// more — one refused during a short outage gets its vector now — and only the
// ones refused again are recorded, so later runs skip them.
func (ing *Ingester) confirmRefusals(ctx context.Context, refused []MissingVector) (done int, confirmed []MissingVector, err error) {
	if len(refused) == 0 {
		return 0, nil, nil
	}
	if err := ing.probeModel(ctx); err != nil {
		return 0, nil, fmt.Errorf("re-embed: %d chunks refused and the model fails a probe: %w", len(refused), err)
	}
	for _, m := range refused {
		d, again, err := ing.reembedGroup(ctx, []MissingVector{m})
		done += d
		confirmed = append(confirmed, again...)
		if err != nil {
			return done, confirmed, err
		}
	}
	ids := make([]int64, len(confirmed))
	for i, m := range confirmed {
		ids[i] = m.ChunkID
	}
	return done, confirmed, ing.store.MarkRefused(ctx, ids)
}

// reembedGroup embeds one owner's chunks in a batch. When the model refuses
// the batch as a bad request, it retries chunk by chunk so only the chunks it
// refuses are left out.
func (ing *Ingester) reembedGroup(ctx context.Context, group []MissingVector) (done int, refused []MissingVector, err error) {
	err = ing.embedAndStore(ctx, group)
	switch {
	case err == nil:
		return len(group), nil, nil
	case !errors.Is(err, llmwire.ErrBadRequest):
		return 0, nil, err
	case len(group) == 1:
		slog.WarnContext(ctx, "rag: embedding model refused a chunk", "chunk_id", group[0].ChunkID, "err", err)
		return 0, group, nil
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

// probeInput is what the model is asked to embed to show it takes input at
// all: short, plain text any embedding model accepts.
const probeInput = "ok"

// probeModel asks the model to embed probeInput. It is a health check of the
// model, not a user's embedding, so no usage is booked on anyone.
func (ing *Ingester) probeModel(ctx context.Context) error {
	_, err := ing.embedder.Embed(inference.WithPurpose(ctx, "embed_probe"), []string{probeInput})
	return err
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
