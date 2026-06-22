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

	res, err := s.Retrieve(ctx, "u1", &p1, nil, vecAt(1, 0), 5)
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

// A thread-private document (composer upload in a project-less thread) must be
// retrievable only within its own thread: visible in thread t1, invisible in a
// different project-less thread t2 and in a project thread. User-global legacy
// chunks stay visible everywhere.
func TestStore_retrieveThreadPrivateScope(t *testing.T) {
	s, _ := newTestStore(t) // seeds user u1 and project p1
	ctx := context.Background()

	t1, p1 := "t1", "p1"

	// Thread-private doc bound to t1 (no project).
	_ = s.CreateDocument(ctx, Document{ID: "dt", UserID: "u1", ThreadID: &t1, VolumeRelpath: "u/dt", Filename: "dt.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dt", []TextChunk{{Ordinal: 0, Text: "thread-private"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("ReplaceChunks dt: %v", err)
	}
	// User-global legacy doc (no project, no thread).
	_ = s.CreateDocument(ctx, Document{ID: "dg", UserID: "u1", VolumeRelpath: "u/dg", Filename: "dg.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dg", []TextChunk{{Ordinal: 0, Text: "global"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("ReplaceChunks dg: %v", err)
	}

	has := func(res []RetrievedChunk, filename string) bool {
		for _, r := range res {
			if r.Filename == filename {
				return true
			}
		}
		return false
	}

	// In its own thread: thread-private doc + global doc are visible.
	inT1, err := s.Retrieve(ctx, "u1", nil, &t1, vecAt(1, 0), 5)
	if err != nil {
		t.Fatalf("Retrieve t1: %v", err)
	}
	if !has(inT1, "dt.txt") {
		t.Errorf("thread-private doc not retrievable in its own thread")
	}
	if !has(inT1, "dg.txt") {
		t.Errorf("global doc not retrievable in thread")
	}

	// In a different project-less thread: only the global doc, never dt.
	t2 := "t2"
	inT2, err := s.Retrieve(ctx, "u1", nil, &t2, vecAt(1, 0), 5)
	if err != nil {
		t.Fatalf("Retrieve t2: %v", err)
	}
	if has(inT2, "dt.txt") {
		t.Errorf("thread-private doc leaked into a different thread")
	}
	if !has(inT2, "dg.txt") {
		t.Errorf("global doc not retrievable in other thread")
	}

	// In a project thread: still no thread-private doc.
	inProj, err := s.Retrieve(ctx, "u1", &p1, &t2, vecAt(1, 0), 5)
	if err != nil {
		t.Fatalf("Retrieve project: %v", err)
	}
	if has(inProj, "dt.txt") {
		t.Errorf("thread-private doc leaked into a project thread")
	}
}
