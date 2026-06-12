package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// vecLit builds a 1536-dimensional vector literal (matching the production
// embedding dimension) whose first component is `first` and the rest are zero.
func vecLit(first float64) string {
	parts := make([]string, 1536)
	parts[0] = "1"
	if first == 0 {
		parts[0] = "0"
	}
	for i := 1; i < 1536; i++ {
		parts[i] = "0"
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestDocumentsRagSchema_tablesExist(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "rag.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"documents", "chunks", "vec_chunks"} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE name=? AND type IN ('table','view')`,
			table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("%s table missing: %v", table, err)
		}
	}
}

func TestDocumentsRagSchema_scopedKNNIsolatesUsers(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "rag.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	for _, u := range []string{"alice", "bob"} {
		if _, err := db.Exec(
			`INSERT INTO users (id, oidc_subject, username, role) VALUES (?, ?, ?, 'user')`,
			u, "subject-"+u, u,
		); err != nil {
			t.Fatalf("insert user %s: %v", u, err)
		}
		if _, err := db.Exec(
			`INSERT INTO documents (id, user_id, volume_relpath, filename, mime, size_bytes, status)
			 VALUES (?, ?, ?, ?, 'text/plain', 3, 'embedded')`,
			"doc-"+u, u, "files/"+u+".txt", u+".txt",
		); err != nil {
			t.Fatalf("insert document %s: %v", u, err)
		}
	}

	// Two chunks with identical embeddings but different owners. A query scoped to
	// alice must never return bob's chunk, even though bob's vector is an exact match.
	insertChunk := func(user, docID string, rowid int64, vec string) {
		t.Helper()
		res, err := db.Exec(
			`INSERT INTO chunks (id, document_id, user_id, ordinal, text, token_count)
			 VALUES (?, ?, ?, 0, 'hello', 1)`,
			rowid, docID, user,
		)
		if err != nil {
			t.Fatalf("insert chunk: %v", err)
		}
		got, err := res.LastInsertId()
		if err != nil || got != rowid {
			t.Fatalf("chunk rowid = %d (err %v), want %d", got, err, rowid)
		}
		if _, err := db.Exec(
			`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id) VALUES (?, ?, ?, '')`,
			rowid, vec, user,
		); err != nil {
			t.Fatalf("insert vec_chunk: %v", err)
		}
	}
	insertChunk("alice", "doc-alice", 1, vecLit(1))
	insertChunk("bob", "doc-bob", 2, vecLit(1))

	var got int64
	err = db.QueryRow(`
		SELECT rowid FROM vec_chunks
		WHERE embedding MATCH ? AND k = 5 AND user_id = ?
		ORDER BY distance`, vecLit(1), "alice").Scan(&got)
	if err != nil {
		t.Fatalf("scoped KNN: %v", err)
	}
	if got != 1 {
		t.Errorf("nearest rowid for alice = %d, want 1 (bob's row must be excluded)", got)
	}
}

func TestDocumentsRagSchema_deleteDocumentCascadesChunks(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "rag.db"))
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(
		`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u', 's', 'u', 'user')`,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO documents (id, user_id, volume_relpath, filename, mime, size_bytes, status)
		 VALUES ('d', 'u', 'files/a.txt', 'a.txt', 'text/plain', 1, 'embedded')`,
	); err != nil {
		t.Fatalf("insert document: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO chunks (id, document_id, user_id, ordinal, text, token_count)
		 VALUES (1, 'd', 'u', 0, 'x', 1)`,
	); err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO vec_chunks (rowid, embedding, user_id, project_id) VALUES (1, ?, 'u', '')`,
		vecLit(1),
	); err != nil {
		t.Fatalf("insert vec_chunk: %v", err)
	}

	// vec0 rows must be removable explicitly (the store does this in the same
	// transaction as chunk deletes, since CASCADE/triggers cannot reach a vtab).
	if _, err := db.Exec(`DELETE FROM vec_chunks WHERE rowid = 1`); err != nil {
		t.Fatalf("explicit vec_chunks delete: %v", err)
	}
	var vecCount int
	if err := db.QueryRow(`SELECT count(*) FROM vec_chunks`).Scan(&vecCount); err != nil {
		t.Fatalf("count vec_chunks: %v", err)
	}
	if vecCount != 0 {
		t.Errorf("vec_chunks after explicit delete = %d, want 0", vecCount)
	}

	// Deleting the document cascades to its chunks via the composite FK.
	if _, err := db.Exec(`DELETE FROM documents WHERE id = 'd'`); err != nil {
		t.Fatalf("delete document: %v", err)
	}
	var chunkCount int
	if err := db.QueryRow(`SELECT count(*) FROM chunks`).Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 0 {
		t.Errorf("chunks after document delete = %d, want 0 (cascade)", chunkCount)
	}
}
