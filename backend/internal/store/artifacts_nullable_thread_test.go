package store

import (
	"path/filepath"
	"testing"
)

func TestArtifacts_threadIDNullableForGlobalUploads(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO threads (id, user_id, title) VALUES ('t','u','T')`); err != nil {
		t.Fatalf("seed thread: %v", err)
	}

	// Thread-less (global) upload: thread_id NULL must be accepted.
	if _, err := db.Exec(
		`INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source)
		 VALUES ('a1','u',NULL,'g.pdf','files/g.pdf','application/pdf',1,'user_uploaded')`,
	); err != nil {
		t.Fatalf("insert thread-less artifact: %v", err)
	}

	// Thread-scoped upload still works and the FK still binds.
	if _, err := db.Exec(
		`INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes, source)
		 VALUES ('a2','u','t','c.pdf','files/c.pdf','application/pdf',1,'user_uploaded')`,
	); err != nil {
		t.Fatalf("insert thread-scoped artifact: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO artifacts (id, user_id, thread_id, display_filename, volume_relpath, mime_type, size_bytes)
		 VALUES ('a3','u','missing','x.pdf','files/x.pdf','application/pdf',1)`,
	); err == nil {
		t.Fatal("artifact with non-existent thread_id inserted, want FK error")
	}

	// documents.artifact_id FK to the rebuilt artifacts table still works.
	if _, err := db.Exec(
		`INSERT INTO documents (id, user_id, artifact_id, volume_relpath, filename, mime, size_bytes, status)
		 VALUES ('d','u','a1','files/g.pdf','g.pdf','application/pdf',1,'pending')`,
	); err != nil {
		t.Fatalf("insert document referencing artifact: %v", err)
	}
}
