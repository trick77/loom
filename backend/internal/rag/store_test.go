package rag

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/trick77/lume/internal/store"
)

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "rag.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u1','s1','u1','user'), ('u2','s2','u2','user')`); err != nil {
		t.Fatalf("seed users: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO projects (id, user_id, name) VALUES ('p1','u1','Project 1')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return NewStore(db), db
}

func unit() []float32 {
	v := make([]float32, 1536)
	v[0] = 1
	return v
}

func TestStore_createGetListDocument(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	doc := Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", SizeBytes: 10, Status: StatusPending}
	if err := s.CreateDocument(ctx, doc); err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	got, ok, err := s.GetDocument(ctx, "u1", "d1")
	if err != nil || !ok {
		t.Fatalf("GetDocument ok=%v err=%v", ok, err)
	}
	if got.Filename != "a.txt" || got.Status != StatusPending {
		t.Errorf("got %+v", got)
	}

	// Cross-user access must not leak.
	if _, ok, _ := s.GetDocument(ctx, "u2", "d1"); ok {
		t.Error("u2 must not see u1's document")
	}

	list, err := s.ListDocuments(ctx, "u1", nil)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListDocuments u1 = %d (err %v), want 1", len(list), err)
	}
	if l2, _ := s.ListDocuments(ctx, "u2", nil); len(l2) != 0 {
		t.Errorf("ListDocuments u2 = %d, want 0", len(l2))
	}
}

func TestStore_updateStatus(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusPending})

	if err := s.UpdateStatus(ctx, "u1", "d1", StatusError, "boom"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _, _ := s.GetDocument(ctx, "u1", "d1")
	if got.Status != StatusError || got.Error != "boom" {
		t.Errorf("status=%q error=%q, want error/boom", got.Status, got.Error)
	}
}

func TestStore_replaceChunksAndRetrieve(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusPending})

	chunks := []TextChunk{{Ordinal: 0, Text: "alpha", TokenCount: 1}, {Ordinal: 1, Text: "beta", TokenCount: 1}}
	embs := [][]float32{unit(), unit()}
	if err := s.ReplaceChunks(ctx, "u1", "d1", chunks, embs); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}

	// Document flips to embedded.
	got, _, _ := s.GetDocument(ctx, "u1", "d1")
	if got.Status != StatusEmbedded {
		t.Errorf("status = %q, want embedded", got.Status)
	}

	// Retrieval returns the indexed chunks for the right user.
	res, err := s.Retrieve(ctx, "u1", nil, nil, unit(), 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("Retrieve returned %d, want 2", len(res))
	}
	if res[0].Filename != "a.txt" || res[0].Text == "" {
		t.Errorf("unexpected result %+v", res[0])
	}

	// Re-indexing replaces, not appends.
	if err := s.ReplaceChunks(ctx, "u1", "d1", chunks[:1], embs[:1]); err != nil {
		t.Fatalf("re-ReplaceChunks: %v", err)
	}
	var chunkCount, vecCount int
	db.QueryRow(`SELECT count(*) FROM chunks WHERE document_id='d1'`).Scan(&chunkCount)
	db.QueryRow(`SELECT count(*) FROM vec_chunks`).Scan(&vecCount)
	if chunkCount != 1 || vecCount != 1 {
		t.Errorf("after re-index chunks=%d vec=%d, want 1/1", chunkCount, vecCount)
	}
}

func TestStore_retrieveIsolatesUsersAndScope(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	// u1 global doc, u1 project doc, u2 doc.
	_ = s.CreateDocument(ctx, Document{ID: "g", UserID: "u1", VolumeRelpath: "files/g.txt", Filename: "g.txt", MIME: "text/plain", Status: StatusPending})
	pid := "p1"
	_ = s.CreateDocument(ctx, Document{ID: "p", UserID: "u1", ProjectID: &pid, VolumeRelpath: "projects/p1/p.txt", Filename: "p.txt", MIME: "text/plain", Status: StatusPending})
	_ = s.CreateDocument(ctx, Document{ID: "o", UserID: "u2", VolumeRelpath: "files/o.txt", Filename: "o.txt", MIME: "text/plain", Status: StatusPending})
	_ = s.ReplaceChunks(ctx, "u1", "g", []TextChunk{{Text: "g"}}, [][]float32{unit()})
	_ = s.ReplaceChunks(ctx, "u1", "p", []TextChunk{{Text: "p"}}, [][]float32{unit()})
	_ = s.ReplaceChunks(ctx, "u2", "o", []TextChunk{{Text: "o"}}, [][]float32{unit()})

	// Project-less thread (nil) sees only u1 global.
	global, _ := s.Retrieve(ctx, "u1", nil, nil, unit(), 10)
	if len(global) != 1 || global[0].Filename != "g.txt" {
		t.Errorf("global retrieval = %+v, want only g.txt", global)
	}

	// Project thread sees project + global, never u2.
	inProject, _ := s.Retrieve(ctx, "u1", &pid, nil, unit(), 10)
	names := map[string]bool{}
	for _, r := range inProject {
		names[r.Filename] = true
	}
	if !names["p.txt"] || !names["g.txt"] || names["o.txt"] || len(inProject) != 2 {
		t.Errorf("project retrieval names = %v, want {p.txt, g.txt}", names)
	}
}

func TestStore_retrieveExcludesNonEmbeddedDocuments(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusPending})
	_ = s.ReplaceChunks(ctx, "u1", "d1", []TextChunk{{Text: "indexed"}}, [][]float32{unit()})

	// Retrievable while embedded.
	if res, _ := s.Retrieve(ctx, "u1", nil, nil, unit(), 5); len(res) != 1 {
		t.Fatalf("embedded doc retrieval = %d, want 1", len(res))
	}

	// A stale document (file vanished) must not contribute chunks even though its
	// chunks/vectors still exist in the index.
	if err := s.UpdateStatus(ctx, "u1", "d1", StatusStale, "file missing"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if res, _ := s.Retrieve(ctx, "u1", nil, nil, unit(), 5); len(res) != 0 {
		t.Errorf("stale doc retrieval = %d, want 0 (excluded)", len(res))
	}
}

func TestStore_deleteDocumentRemovesChunksAndVectors(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	_ = s.CreateDocument(ctx, Document{ID: "d1", UserID: "u1", VolumeRelpath: "files/a.txt", Filename: "a.txt", MIME: "text/plain", Status: StatusPending})
	_ = s.ReplaceChunks(ctx, "u1", "d1", []TextChunk{{Text: "x"}}, [][]float32{unit()})

	if err := s.DeleteDocument(ctx, "u1", "d1"); err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	var docs, chunks, vecs int
	db.QueryRow(`SELECT count(*) FROM documents`).Scan(&docs)
	db.QueryRow(`SELECT count(*) FROM chunks`).Scan(&chunks)
	db.QueryRow(`SELECT count(*) FROM vec_chunks`).Scan(&vecs)
	if docs != 0 || chunks != 0 || vecs != 0 {
		t.Errorf("after delete docs=%d chunks=%d vecs=%d, want 0/0/0", docs, chunks, vecs)
	}
}
