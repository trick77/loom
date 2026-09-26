package rag

import (
	"context"
	"testing"
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
	if missing, err := s.ChunksMissingVectors(ctx, 10); err != nil || len(missing) != 0 {
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
