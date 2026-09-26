package rag

import (
	"context"
	"testing"

	"github.com/trick77/llmwire"
)

// After the vector table is rebuilt, re-embedding reads the stored chunk text,
// writes every missing vector, and books the embedding usage on the owner.
func TestIngester_ReembedMissingRestoresEveryVector(t *testing.T) {
	emb := &fakeEmbedder{}
	usage := &fakeUsageRecorder{}
	ing, s := newIngester(t, fakeExtractor{}, emb, fakeOpener{})
	ing.SetUsageRecorder(usage)
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha", "beta", "gamma")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatalf("RebuildVectorTable: %v", err)
	}

	n, err := ing.ReembedMissing(ctx)
	if err != nil {
		t.Fatalf("ReembedMissing: %v", err)
	}
	if n != 3 {
		t.Fatalf("re-embedded %d chunks, want 3", n)
	}
	if missing, err := s.ChunksMissingVectors(ctx, 0, 10); err != nil || len(missing) != 0 {
		t.Fatalf("still missing = %v, %v", missing, err)
	}
	if len(emb.gotInputs) != 1 || len(emb.gotInputs[0]) != 3 || emb.gotInputs[0][0] != "alpha" {
		t.Fatalf("embedded inputs = %v, want the stored chunk text", emb.gotInputs)
	}
	if usage.userID != "u1" || usage.tokens != 3 || usage.requests != 1 {
		t.Fatalf("usage = %+v, want u1, 3 tokens, 1 request", usage)
	}
	res, err := s.Retrieve(ctx, "u1", nil, nil, unit(), 10)
	if err != nil || len(res) != 3 {
		t.Fatalf("retrieved %d, %v; want all 3 chunks back", len(res), err)
	}
}

// poisonEmbedder refuses any batch containing "poison" the way an endpoint
// refuses an input it will never take (a bad request, not a transient error).
type poisonEmbedder struct{ fakeEmbedder }

func (p *poisonEmbedder) Embed(ctx context.Context, inputs []string) (EmbedResult, error) {
	for _, in := range inputs {
		if in == "poison" {
			return EmbedResult{}, &llmwire.APIError{StatusCode: 400, Message: "input too long", Class: llmwire.ErrBadRequest}
		}
	}
	return p.fakeEmbedder.Embed(ctx, inputs)
}

// One chunk the model will never take must not stall re-embedding: it is
// skipped, every other chunk still gets its vector, and the run completes.
func TestIngester_ReembedMissingSkipsAChunkTheModelRefuses(t *testing.T) {
	ing, s := newIngester(t, fakeExtractor{}, &poisonEmbedder{}, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha", "poison", "gamma")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}

	n, err := ing.ReembedMissing(ctx)
	if err != nil {
		t.Fatalf("ReembedMissing: %v", err)
	}
	if n != 2 {
		t.Fatalf("re-embedded %d chunks, want the 2 the model takes", n)
	}
	// The refused chunk is recorded as refused, so nothing is left to re-embed.
	if missing, err := s.ChunksMissingVectors(ctx, 0, 10); err != nil || len(missing) != 0 {
		t.Fatalf("still missing = %+v, %v; want none (the refused chunk is recorded)", missing, err)
	}
	var refused string
	if err := s.db.QueryRowContext(ctx, `SELECT c.text FROM vector_refused r JOIN chunks c ON c.id = r.chunk_id`).Scan(&refused); err != nil || refused != "poison" {
		t.Fatalf("refused = %q, %v; want the poison chunk", refused, err)
	}
}

// A model that refuses every input (it rejects the model or its parameters)
// is not a finished run: it errors so the caller retries, instead of
// declaring the corpus done while none of it is retrievable.
func TestIngester_ReembedMissingFailsWhenEveryChunkIsRefused(t *testing.T) {
	ing, s := newIngester(t, fakeExtractor{}, &poisonEmbedder{}, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "poison")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.ReembedMissing(ctx); err == nil {
		t.Fatal("ReembedMissing succeeded while every chunk was refused")
	}
}

// Each embedding call stores its vectors right away: a refused batch never
// throws away the batches before it (they were already billed).
func TestIngester_ReembedMissingKeepsBatchesBeforeARefusal(t *testing.T) {
	emb := &poisonEmbedder{}
	ing, s := newIngester(t, fakeExtractor{}, emb, fakeOpener{})
	ctx := context.Background()
	texts := make([]string, embedBatchSize+1)
	for i := range texts {
		texts[i] = "chunk"
	}
	texts[embedBatchSize] = "poison"
	seedEmbeddedDocument(t, s, "d1", texts...)
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.ReembedMissing(ctx); err != nil {
		t.Fatalf("ReembedMissing: %v", err)
	}
	// One call for the first full batch, one refused call, one retry of the
	// refused chunk alone: never the first batch again.
	embedded := 0
	for _, in := range emb.gotInputs {
		embedded += len(in)
	}
	if embedded != embedBatchSize {
		t.Fatalf("embedded %d inputs, want exactly the %d of the first batch once", embedded, embedBatchSize)
	}
}

// A chunk the model refused in a run that embedded others is refused for good:
// later runs skip it instead of failing on it forever.
func TestIngester_ReembedMissingRemembersRefusedChunks(t *testing.T) {
	emb := &poisonEmbedder{}
	ing, s := newIngester(t, fakeExtractor{}, emb, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha", "poison")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.ReembedMissing(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	calls := len(emb.gotInputs)
	n, err := ing.ReembedMissing(ctx)
	if err != nil || n != 0 {
		t.Fatalf("second run: n=%d err=%v, want a no-op", n, err)
	}
	if len(emb.gotInputs) != calls {
		t.Fatal("second run re-sent the refused chunk")
	}

	// A rebuild (another model) gives the refused chunk another chance.
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	missing, err := s.ChunksMissingVectors(ctx, 0, 10)
	if err != nil || len(missing) != 2 {
		t.Fatalf("after rebuild: missing = %+v, %v; want both chunks again", missing, err)
	}
}

// When the only chunks left without a vector are ones the model refuses (an
// interrupted run, or an upgrade after the old code skipped them), the model
// still works — other vectors exist — so the refusals are recorded instead of
// failing every retry forever.
func TestIngester_ReembedMissingRecordsRefusalsWhenOnlyThoseRemain(t *testing.T) {
	ing, s := newIngester(t, fakeExtractor{}, &poisonEmbedder{}, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha")
	seedEmbeddedDocument(t, s, "d2", "poison")
	if _, err := s.db.ExecContext(ctx, `DELETE FROM vec_chunks WHERE rowid = (SELECT id FROM chunks WHERE text = 'poison')`); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.ReembedMissing(ctx); err != nil {
		t.Fatalf("ReembedMissing: %v, want the refusal recorded", err)
	}
	if n, err := ing.ReembedMissing(ctx); err != nil || n != 0 {
		t.Fatalf("second run: n=%d err=%v, want a no-op", n, err)
	}
}

// Clearing a document forgets its refused chunks: their ids can be reused.
func TestStore_ClearChunksForgetsRefusals(t *testing.T) {
	ing, s := newIngester(t, fakeExtractor{}, &poisonEmbedder{}, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha", "poison")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.ReembedMissing(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearChunks(ctx, "u1", "d1"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM vector_refused`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("refusals left after clear = %d, %v; want 0", n, err)
	}
}

// A transient failure (the upstream is down) ends the run so it is retried.
func TestIngester_ReembedMissingStopsOnATransientError(t *testing.T) {
	ing, s := newIngester(t, fakeExtractor{}, &fakeEmbedder{err: &llmwire.APIError{StatusCode: 503, Class: llmwire.ErrUpstream}}, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.ReembedMissing(ctx); err == nil {
		t.Fatal("ReembedMissing succeeded against a failing upstream")
	}
}

// Nothing missing is the steady state at every boot: no embedding call.
func TestIngester_ReembedMissingIsANoOpWhenComplete(t *testing.T) {
	emb := &fakeEmbedder{}
	ing, s := newIngester(t, fakeExtractor{}, emb, fakeOpener{})
	seedEmbeddedDocument(t, s, "d1", "alpha")

	n, err := ing.ReembedMissing(context.Background())
	if err != nil || n != 0 || len(emb.gotInputs) != 0 {
		t.Fatalf("n = %d, err = %v, calls = %d; want a no-op", n, err, len(emb.gotInputs))
	}
}
