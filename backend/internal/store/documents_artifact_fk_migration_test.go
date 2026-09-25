package store

import (
	"path/filepath"
	"testing"
)

// The composite (user_id, artifact_id) foreign key on documents carried
// ON DELETE SET NULL, which on an artifact delete would null user_id too and
// abort on its NOT NULL constraint; the delete path worked around it by
// detaching artifacts first. The rebuild keys artifact_id alone. Because the
// rebuild drops a table that chunks cascade from, this is also the test that
// the runner's foreign-keys-off directive keeps existing chunks alive.
func TestMigration0029_documentsArtifactFKIsSingleColumnAndKeepsChunks(t *testing.T) {
	db := openMigratedUpTo(t, filepath.Join(t.TempDir(), "a.db"), "0028_shared_threads_user_scoped_fk.sql")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`)
	exec(`INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source)
	      VALUES ('a1','u',NULL,'g.pdf','files/g.pdf','application/pdf',1,'user_uploaded')`)
	exec(`INSERT INTO documents (id, user_id, artifact_id, volume_relpath, filename, mime, size_bytes, status, full_text)
	      VALUES ('d','u','a1','files/g.pdf','g.pdf','application/pdf',1,'embedded','hello')`)
	exec(`INSERT INTO chunks (document_id, user_id, ordinal, text) VALUES ('d','u',0,'hello')`)

	if err := migrate(db); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM chunks WHERE document_id = 'd'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("chunks after rebuild = %d (err %v), want 1: the rebuild cascaded into chunks", n, err)
	}
	var fullText string
	if err := db.QueryRow(`SELECT full_text FROM documents WHERE id = 'd'`).Scan(&fullText); err != nil || fullText != "hello" {
		t.Fatalf("full_text after rebuild = %q (err %v), want hello", fullText, err)
	}

	// Deleting the artifact now detaches the document instead of aborting.
	exec(`DELETE FROM artifacts WHERE id = 'a1'`)
	var userID string
	var artifactID *string
	if err := db.QueryRow(`SELECT user_id, artifact_id FROM documents WHERE id = 'd'`).Scan(&userID, &artifactID); err != nil {
		t.Fatalf("document after artifact delete: %v", err)
	}
	if userID != "u" || artifactID != nil {
		t.Fatalf("document after artifact delete = user %q, artifact %v; want user kept and artifact NULL", userID, artifactID)
	}

	for _, idx := range []string{"idx_documents_user_created", "idx_documents_user_project", "idx_documents_status", "idx_documents_user_thread", "idx_documents_user_artifact"} {
		if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, idx).Scan(&n); err != nil || n != 1 {
			t.Fatalf("index %s present = %d (err %v), want 1", idx, n, err)
		}
	}
	// Foreign keys are back on for the connection pool.
	if _, err := db.Exec(`INSERT INTO chunks (document_id, user_id, ordinal, text) VALUES ('missing','u',0,'x')`); err == nil {
		t.Fatal("chunk for a missing document inserted: foreign keys were left off")
	}
}
