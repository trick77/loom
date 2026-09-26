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
	missing, err := s.ChunksMissingVectors(ctx, 0, 10)
	if err != nil || len(missing) != 1 || missing[0].Text != "poison" {
		t.Fatalf("still missing = %+v, %v; want only the refused chunk", missing, err)
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
