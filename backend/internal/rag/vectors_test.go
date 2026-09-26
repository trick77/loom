package rag

import (
	"context"
	"testing"
)

func seedEmbeddedDocument(t *testing.T, s *Store, id string, texts ...string) {
	t.Helper()
	ctx := context.Background()
	doc := Document{ID: id, UserID: "u1", VolumeRelpath: "files/" + id + ".txt", Filename: id + ".txt", MIME: "text/plain", SizeBytes: 10, Status: StatusPending}
	if err := s.CreateDocument(ctx, doc); err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}
	chunks := make([]TextChunk, len(texts))
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		chunks[i] = TextChunk{Ordinal: i, Text: text}
		vectors[i] = unit()
	}
	if err := s.ReplaceChunks(ctx, "u1", id, chunks, vectors); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}
}

// Re-embedding lists chunks, embeds for seconds, then inserts. A document
// cleared or re-indexed meanwhile must not get a stale vector, and must not
// fail the batch: those chunks are skipped.
func TestStore_InsertVectorsSkipsChunksChangedMeanwhile(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha")
	seedEmbeddedDocument(t, s, "d2", "beta")
	if err := s.RebuildVectorTable(ctx, len(unit())); err != nil {
		t.Fatal(err)
	}
	missing, err := s.ChunksMissingVectors(ctx, 10)
	if err != nil || len(missing) != 2 {
		t.Fatalf("missing = %v, %v", missing, err)
	}

	// d1 is unindexed (chunks gone); d2 is re-indexed (new chunks, own vectors).
	if err := s.ClearChunks(ctx, "u1", "d1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceChunks(ctx, "u1", "d2", []TextChunk{{Ordinal: 0, Text: "beta v2"}}, [][]float32{unit()}); err != nil {
		t.Fatal(err)
	}

	vectors := [][]float32{unit(), unit()}
	if err := s.InsertVectors(ctx, missing, vectors); err != nil {
		t.Fatalf("InsertVectors: %v", err)
	}
	res, err := s.Retrieve(ctx, "u1", nil, nil, unit(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Text != "beta v2" {
		t.Fatalf("retrieved %+v, want only d2's re-indexed chunk", res)
	}
}

func TestStore_VectorWidthReadsTheTable(t *testing.T) {
	s, _ := newTestStore(t)
	width, err := s.VectorWidth(context.Background())
	if err != nil {
		t.Fatalf("VectorWidth: %v", err)
	}
	if width != len(unit()) {
		t.Fatalf("width = %d, want %d", width, len(unit()))
	}
}

// A new embedding model with another width: the vector table is rebuilt at the
// new width, the chunk text stays, and every chunk is listed as missing its
// vector until it is re-embedded.
func TestStore_RebuildVectorsKeepsChunksAndListsThemMissing(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	seedEmbeddedDocument(t, s, "d1", "alpha", "beta")

	if missing, err := s.ChunksMissingVectors(ctx, 10); err != nil || len(missing) != 0 {
		t.Fatalf("before rebuild: missing = %v, %v; want none", missing, err)
	}
	if err := s.RebuildVectorTable(ctx, 8); err != nil {
		t.Fatalf("RebuildVectorTable: %v", err)
	}
	if width, err := s.VectorWidth(ctx); err != nil || width != 8 {
		t.Fatalf("width after rebuild = %d, %v; want 8", width, err)
	}
	missing, err := s.ChunksMissingVectors(ctx, 10)
	if err != nil {
		t.Fatalf("ChunksMissingVectors: %v", err)
	}
	if len(missing) != 2 || missing[0].Text != "alpha" || missing[1].Text != "beta" || missing[0].UserID != "u1" {
		t.Fatalf("missing = %+v, want both chunks of d1", missing)
	}

	vectors := make([][]float32, len(missing))
	for i := range vectors {
		v := make([]float32, 8)
		v[0] = 1
		vectors[i] = v
	}
	if err := s.InsertVectors(ctx, missing, vectors); err != nil {
		t.Fatalf("InsertVectors: %v", err)
	}
	if missing, err := s.ChunksMissingVectors(ctx, 10); err != nil || len(missing) != 0 {
		t.Fatalf("after re-embed: missing = %v, %v; want none", missing, err)
	}
	query := make([]float32, 8)
	query[0] = 1
	got, err := s.Retrieve(ctx, "u1", nil, nil, query, 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("retrieved %d chunks, want 2 at the new width", len(got))
	}
}
