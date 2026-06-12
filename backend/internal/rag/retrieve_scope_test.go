package rag

import (
	"context"
	"testing"
)

// vecAt builds a 1536-dim vector with a given value in dim 0 and dim 1, so we
// can place chunks at controlled distances from a query.
func vecAt(d0, d1 float32) []float32 {
	v := make([]float32, 1536)
	v[0] = d0
	v[1] = d1
	return v
}

// This is the discriminator for the sqlite-vec landmine: does the project_id
// metadata `IN (...)` filter compose with `k`, or is it applied as a post-filter
// AFTER the k nearest are chosen? We seed many exact-match chunks in an
// out-of-scope project (p2) and a few slightly-farther chunks in the in-scope
// project (p1). If the filter is a post-filter, the k nearest are all p2 and get
// filtered away, leaving p1 invisible. A correct scoped retrieval still returns
// the p1 chunks.
func TestStore_retrieveScopeComposesWithK(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO projects (id, user_id, name) VALUES ('p2','u1','Project 2')`); err != nil {
		t.Fatalf("seed p2: %v", err)
	}

	p1, p2 := "p1", "p2"
	// 50 exact-match (distance ~0) chunks in the OUT-of-scope project p2.
	_ = s.CreateDocument(ctx, Document{ID: "d2", UserID: "u1", ProjectID: &p2, VolumeRelpath: "projects/p2/d2", Filename: "d2.txt", MIME: "text/plain", Status: StatusPending})
	chunks2 := make([]TextChunk, 50)
	embs2 := make([][]float32, 50)
	for i := range chunks2 {
		chunks2[i] = TextChunk{Ordinal: i, Text: "out-of-scope", TokenCount: 1}
		embs2[i] = vecAt(1, 0)
	}
	if err := s.ReplaceChunks(ctx, "u1", "d2", chunks2, embs2); err != nil {
		t.Fatalf("ReplaceChunks p2: %v", err)
	}

	// 3 slightly-farther chunks in the IN-scope project p1.
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", ProjectID: &p1, VolumeRelpath: "projects/p1/d1", Filename: "d1.txt", MIME: "text/plain", Status: StatusPending})
	chunks1 := []TextChunk{{Ordinal: 0, Text: "scope-a"}, {Ordinal: 1, Text: "scope-b"}, {Ordinal: 2, Text: "scope-c"}}
	embs1 := [][]float32{vecAt(1, 0.1), vecAt(1, 0.1), vecAt(1, 0.1)}
	if err := s.ReplaceChunks(ctx, "u1", "d1", chunks1, embs1); err != nil {
		t.Fatalf("ReplaceChunks p1: %v", err)
	}

	res, err := s.Retrieve(ctx, "u1", &p1, vecAt(1, 0), 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	got := 0
	for _, r := range res {
		if r.Filename == "d1.txt" {
			got++
		}
		if r.Filename == "d2.txt" {
			t.Errorf("out-of-scope chunk leaked into project retrieval: %+v", r)
		}
	}
	if got != 3 {
		t.Errorf("in-scope chunks returned = %d, want 3 (scope filter must compose with k, not post-filter)", got)
	}
}
