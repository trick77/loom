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
	// One call for the first full batch, one refused call, then a one-input
	// probe of a chunk the model already took: never the first batch again.
	embedded := 0
	for _, in := range emb.gotInputs {
		embedded += len(in)
	}
	if embedded != embedBatchSize+1 {
		t.Fatalf("embedded %d inputs, want the %d of the first batch once plus the probe", embedded, embedBatchSize)
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

// switchableEmbedder refuses every input as a bad request once down is set: a
// retired model, a billing error — a failure of the model, not of a chunk.
type switchableEmbedder struct {
	fakeEmbedder
	down bool
}

func (e *switchableEmbedder) Embed(ctx context.Context, inputs []string) (EmbedResult, error) {
	if e.down {
		return EmbedResult{}, &llmwire.APIError{StatusCode: 400, Message: "model not found", Class: llmwire.ErrBadRequest}
	}
	return e.fakeEmbedder.Embed(ctx, inputs)
}

// A model that starts refusing everything after vectors exist is an outage,
// not a set of bad chunks: nothing is recorded as refused and the run fails,
// so it is retried once the model is back.
func TestIngester_ReembedMissingDoesNotRecordAnOutageAsRefusals(t *testing.T) {
	emb := &switchableEmbedder{}
	ing, s := newIngester(t, fakeExtractor{}, emb, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha")
	seedEmbeddedDocument(t, s, "d2", "beta")
	if _, err := s.db.ExecContext(ctx, `DELETE FROM vec_chunks WHERE rowid = (SELECT id FROM chunks WHERE text = 'beta')`); err != nil {
		t.Fatal(err)
	}
	emb.down = true
	if _, err := ing.ReembedMissing(ctx); err == nil {
		t.Fatal("ReembedMissing succeeded while the model refused everything")
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM vector_refused`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("recorded %d refusals during an outage, want 0", n)
	}
	emb.down = false
	if got, err := ing.ReembedMissing(ctx); err != nil || got != 1 {
		t.Fatalf("after recovery: n=%d err=%v, want the chunk embedded", got, err)
	}
}

// Right after a rebuild no chunk has a vector. If the only chunk is one the
// model refuses, the run still settles: the probe needs no stored chunk, and
// the refusal is recorded instead of failing every retry.
func TestIngester_ReembedMissingSettlesWhenOnlyARefusedChunkExists(t *testing.T) {
	ing, s := newIngester(t, fakeExtractor{}, &poisonEmbedder{}, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "poison")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.ReembedMissing(ctx); err != nil {
		t.Fatalf("ReembedMissing: %v, want the refusal recorded", err)
	}
	if n, err := ing.ReembedMissing(ctx); err != nil || n != 0 {
		t.Fatalf("second run: n=%d err=%v, want a no-op", n, err)
	}
}

// flakyEmbedder refuses every input as a bad request for its first failures
// calls, then works: a short outage, not bad chunks.
type flakyEmbedder struct {
	fakeEmbedder
	failures int
}

func (e *flakyEmbedder) Embed(ctx context.Context, inputs []string) (EmbedResult, error) {
	if e.failures > 0 {
		e.failures--
		return EmbedResult{}, &llmwire.APIError{StatusCode: 400, Message: "temporarily refused", Class: llmwire.ErrBadRequest}
	}
	return e.fakeEmbedder.Embed(ctx, inputs)
}

// A chunk refused during a short outage is retried once the model answers the
// probe; it gets its vector and is never recorded as refused.
func TestIngester_ReembedMissingConfirmsRefusalsAfterTheProbe(t *testing.T) {
	ing, s := newIngester(t, fakeExtractor{}, &flakyEmbedder{failures: 1}, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	n, err := ing.ReembedMissing(ctx)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v, want the chunk embedded on confirmation", n, err)
	}
	var refused int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM vector_refused`).Scan(&refused); err != nil || refused != 0 {
		t.Fatalf("recorded %d refusals, want 0", refused)
	}
}

// The probe is a health check of the model, not a user's embedding: it is
// never booked on anyone.
func TestIngester_ProbeIsNotBilled(t *testing.T) {
	usage := &fakeUsageRecorder{}
	ing, s := newIngester(t, fakeExtractor{}, &poisonEmbedder{}, fakeOpener{})
	ing.SetUsageRecorder(usage)
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "poison")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.ReembedMissing(ctx); err != nil {
		t.Fatal(err)
	}
	if usage.calls != 0 {
		t.Fatalf("usage recorded %d times, want none (only the probe succeeded)", usage.calls)
	}
}

// Every way a chunk disappears (thread or project delete, cascades) forgets its
// refusal, not only ClearChunks: a new chunk can reuse the id.
func TestStore_AnyChunkDeleteForgetsItsRefusal(t *testing.T) {
	ing, s := newIngester(t, fakeExtractor{}, &poisonEmbedder{}, fakeOpener{})
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha", "poison")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	if _, err := ing.ReembedMissing(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM chunks WHERE text = 'poison'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM vector_refused`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("refusals left after a direct chunk delete = %d, %v; want 0", n, err)
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
