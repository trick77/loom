package rag

import (
	"context"
	"database/sql"
	"testing"
)

func chunkRowids(t *testing.T, db *sql.DB, documentID string) []int64 {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM chunks WHERE document_id = ?`, documentID)
	if err != nil {
		t.Fatalf("query chunk rowids: %v", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan rowid: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

func countRow(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestStore_DeleteThreadScopeDocuments(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()

	t1, t2 := "t1", "t2"
	pid := "p1"
	// Thread-private doc in t1 (project NULL, thread set) — must be deleted.
	_ = s.CreateDocument(ctx, Document{ID: "dT1", UserID: "u1", ThreadID: &t1, VolumeRelpath: "u/t1.txt", Filename: "t1.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dT1", []TextChunk{{Text: "thread one"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dT1: %v", err)
	}
	// Sibling thread-private doc in t2 — must survive.
	_ = s.CreateDocument(ctx, Document{ID: "dT2", UserID: "u1", ThreadID: &t2, VolumeRelpath: "u/t2.txt", Filename: "t2.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dT2", []TextChunk{{Text: "thread two"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dT2: %v", err)
	}
	// Project doc — must survive.
	_ = s.CreateDocument(ctx, Document{ID: "dP", UserID: "u1", ProjectID: &pid, VolumeRelpath: "projects/p1/p.txt", Filename: "p.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dP", []TextChunk{{Text: "project"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dP: %v", err)
	}

	t1Rowids := chunkRowids(t, db, "dT1")
	if len(t1Rowids) == 0 {
		t.Fatal("expected dT1 chunks before delete")
	}

	if err := s.DeleteThreadScopeDocuments(ctx, "u1", t1); err != nil {
		t.Fatalf("DeleteThreadScopeDocuments: %v", err)
	}

	// dT1 document, chunks, embeddings all gone.
	if _, ok, _ := s.GetDocument(ctx, "u1", "dT1"); ok {
		t.Error("dT1 document not deleted")
	}
	if n := countRow(t, db, `SELECT count(*) FROM chunks WHERE document_id = 'dT1'`); n != 0 {
		t.Errorf("dT1 chunks remain: %d", n)
	}
	for _, id := range t1Rowids {
		if n := countRow(t, db, `SELECT count(*) FROM vec_chunks WHERE rowid = ?`, id); n != 0 {
			t.Errorf("vec_chunks rowid %d not deleted", id)
		}
	}
	if res, _ := s.Retrieve(ctx, "u1", nil, &t1, vecAt(1, 0), 10); len(res) != 0 {
		t.Errorf("dT1 still retrievable in t1: %+v", res)
	}
	// Sibling thread and project doc survive.
	if _, ok, _ := s.GetDocument(ctx, "u1", "dT2"); !ok {
		t.Error("dT2 wrongly deleted")
	}
	if _, ok, _ := s.GetDocument(ctx, "u1", "dP"); !ok {
		t.Error("dP wrongly deleted")
	}
}

// A project-less thread with a private attachment can be moved into a project; the
// document keeps its thread scope (project_id NULL, thread_id set). Deleting the
// project must still purge it, even though it does not match the project_id
// filter and has no FK to ride the cascade.
func TestStore_DeleteProjectScopeDocuments_MovedThreadAttachment(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	pid := "p1"

	// Thread thr_moved now belongs to project p1, but its attachment stayed
	// thread-scoped (uploaded while the chat had no project).
	if _, err := db.Exec(`INSERT INTO threads (id, user_id, project_id, title) VALUES ('thr_moved','u1','p1','Moved')`); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	tid := "thr_moved"
	_ = s.CreateDocument(ctx, Document{ID: "dMoved", UserID: "u1", ThreadID: &tid, VolumeRelpath: "u/m.txt", Filename: "m.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dMoved", []TextChunk{{Text: "moved"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dMoved: %v", err)
	}
	// A thread-private doc in an unrelated, project-less thread — must survive.
	other := "thr_other"
	_ = s.CreateDocument(ctx, Document{ID: "dOther", UserID: "u1", ThreadID: &other, VolumeRelpath: "u/o.txt", Filename: "o.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dOther", []TextChunk{{Text: "other"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dOther: %v", err)
	}

	movedRowids := chunkRowids(t, db, "dMoved")
	if len(movedRowids) == 0 {
		t.Fatal("expected dMoved chunks before delete")
	}

	if err := s.DeleteProjectScopeDocuments(ctx, "u1", pid); err != nil {
		t.Fatalf("DeleteProjectScopeDocuments: %v", err)
	}

	if _, ok, _ := s.GetDocument(ctx, "u1", "dMoved"); ok {
		t.Error("moved-thread attachment not deleted")
	}
	for _, id := range movedRowids {
		if n := countRow(t, db, `SELECT count(*) FROM vec_chunks WHERE rowid = ?`, id); n != 0 {
			t.Errorf("vec_chunks rowid %d not deleted", id)
		}
	}
	if _, ok, _ := s.GetDocument(ctx, "u1", "dOther"); !ok {
		t.Error("unrelated thread-private doc wrongly deleted")
	}
}

func TestStore_DeleteProjectScopeDocuments(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	pid := "p1"

	// Project doc with embeddings — must be deleted.
	_ = s.CreateDocument(ctx, Document{ID: "dP", UserID: "u1", ProjectID: &pid, VolumeRelpath: "projects/p1/p.txt", Filename: "p.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dP", []TextChunk{{Text: "project"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dP: %v", err)
	}
	// Unrelated project-less thread doc — must survive.
	tid := "t9"
	_ = s.CreateDocument(ctx, Document{ID: "dT", UserID: "u1", ThreadID: &tid, VolumeRelpath: "u/t.txt", Filename: "t.txt", MIME: "text/plain", Status: StatusPending})
	if err := s.ReplaceChunks(ctx, "u1", "dT", []TextChunk{{Text: "thread"}}, [][]float32{vecAt(1, 0)}); err != nil {
		t.Fatalf("seed dT: %v", err)
	}

	pRowids := chunkRowids(t, db, "dP")
	if len(pRowids) == 0 {
		t.Fatal("expected dP chunks before delete")
	}

	if err := s.DeleteProjectScopeDocuments(ctx, "u1", pid); err != nil {
		t.Fatalf("DeleteProjectScopeDocuments: %v", err)
	}

	if _, ok, _ := s.GetDocument(ctx, "u1", "dP"); ok {
		t.Error("dP document not deleted")
	}
	if n := countRow(t, db, `SELECT count(*) FROM chunks WHERE document_id = 'dP'`); n != 0 {
		t.Errorf("dP chunks remain: %d", n)
	}
	for _, id := range pRowids {
		if n := countRow(t, db, `SELECT count(*) FROM vec_chunks WHERE rowid = ?`, id); n != 0 {
			t.Errorf("vec_chunks rowid %d not deleted", id)
		}
	}
	if res, _ := s.Retrieve(ctx, "u1", &pid, nil, vecAt(1, 0), 10); len(res) != 0 {
		t.Errorf("dP still retrievable in project: %+v", res)
	}
	if _, ok, _ := s.GetDocument(ctx, "u1", "dT"); !ok {
		t.Error("unrelated thread doc dT wrongly deleted")
	}
}
