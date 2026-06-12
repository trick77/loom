package rag

import (
	"context"
	"testing"
)

// Reconciliation of pre-thread-scoping uploads: a global document still linked to
// its source thread (via its artifact) is rebound and isolated to that thread; a
// global document with no recoverable origin is deleted; project documents are
// untouched. Running it twice is a no-op (idempotent).
func TestStore_reconcileLegacyDocumentScopes(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()

	// Two project-less threads and the project from newTestStore (p1).
	if _, err := db.Exec(`INSERT INTO threads (id, user_id, title) VALUES ('t1','u1','T1'), ('t2','u1','T2')`); err != nil {
		t.Fatalf("seed threads: %v", err)
	}
	// Artifact recording that the recoverable doc came from thread t1.
	if _, err := db.Exec(`INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source)
		VALUES ('aR','u1','t1','rec.txt','u/rec.txt','text/plain',1,'user_uploaded')`); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}

	aR := "aR"
	// Legacy shape: project_id NULL, thread_id NULL, embeddings keyed global ('').
	_ = s.CreateDocument(ctx, Document{ID: "dR", UserID: "u1", ArtifactID: &aR, VolumeRelpath: "u/rec.txt", Filename: "rec.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dR", []TextChunk{{Text: "recoverable"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dR chunks: %v", err)
	}
	// Unrecoverable global: no artifact link, so its origin chat is unknown.
	_ = s.CreateDocument(ctx, Document{ID: "dU", UserID: "u1", VolumeRelpath: "u/unk.txt", Filename: "unk.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dU", []TextChunk{{Text: "unrecoverable"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dU chunks: %v", err)
	}
	// Project document: must be left alone.
	pid := "p1"
	_ = s.CreateDocument(ctx, Document{ID: "dP", UserID: "u1", ProjectID: &pid, VolumeRelpath: "p1/proj.txt", Filename: "proj.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dP", []TextChunk{{Text: "project"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dP chunks: %v", err)
	}

	run := func() {
		if err := s.ReconcileLegacyDocumentScopes(ctx); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}
	run()

	has := func(res []RetrievedChunk, filename string) bool {
		for _, r := range res {
			if r.Filename == filename {
				return true
			}
		}
		return false
	}
	t1, t2 := "t1", "t2"

	// Recoverable doc is now bound to t1: retrievable there, nowhere else.
	if doc, ok, _ := s.GetDocument(ctx, "u1", "dR"); !ok || doc.ThreadID == nil || *doc.ThreadID != "t1" {
		t.Errorf("dR thread_id = %v, want t1", func() any {
			if doc.ThreadID == nil {
				return nil
			}
			return *doc.ThreadID
		}())
	}
	inT1, _ := s.Retrieve(ctx, "u1", nil, &t1, vecAt(1, 0), 10)
	if !has(inT1, "rec.txt") {
		t.Errorf("recovered doc not retrievable in its own thread t1")
	}
	inT2, _ := s.Retrieve(ctx, "u1", nil, &t2, vecAt(1, 0), 10)
	if has(inT2, "rec.txt") {
		t.Errorf("recovered doc still leaks into unrelated thread t2")
	}

	// Unrecoverable global is gone (document + chunks + embeddings).
	if _, ok, _ := s.GetDocument(ctx, "u1", "dU"); ok {
		t.Errorf("unrecoverable global document was not deleted")
	}
	var chunkCount int
	_ = db.QueryRow(`SELECT count(*) FROM chunks WHERE document_id='dU'`).Scan(&chunkCount)
	if chunkCount != 0 {
		t.Errorf("unrecoverable doc chunks remain: %d", chunkCount)
	}
	if has(inT1, "unk.txt") || has(inT2, "unk.txt") {
		t.Errorf("deleted global doc still retrievable")
	}

	// Project document untouched and still project-scoped.
	if doc, ok, _ := s.GetDocument(ctx, "u1", "dP"); !ok || doc.ThreadID != nil || doc.ProjectID == nil {
		t.Errorf("project document was altered: %+v", doc)
	}
	inProject, _ := s.Retrieve(ctx, "u1", &pid, &t2, vecAt(1, 0), 10)
	if !has(inProject, "proj.txt") {
		t.Errorf("project document no longer retrievable in its project")
	}

	// One-time: a global document created AFTER the first reconcile (e.g. a future
	// deliberate global-upload feature) must survive a later run untouched.
	_ = s.CreateDocument(ctx, Document{ID: "dG2", UserID: "u1", VolumeRelpath: "u/g2.txt", Filename: "g2.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dG2", []TextChunk{{Text: "future global"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dG2 chunks: %v", err)
	}
	run()
	if _, ok, _ := s.GetDocument(ctx, "u1", "dG2"); !ok {
		t.Errorf("global document created after the one-time reconcile was wrongly deleted")
	}
	// And the original recovery still holds.
	if doc, ok, _ := s.GetDocument(ctx, "u1", "dR"); !ok || doc.ThreadID == nil || *doc.ThreadID != "t1" {
		t.Errorf("dR scope changed on second reconcile run")
	}
	inT1b, _ := s.Retrieve(ctx, "u1", nil, &t1, vecAt(1, 0), 10)
	if !has(inT1b, "rec.txt") {
		t.Errorf("recovered doc not retrievable after second reconcile run")
	}
}
