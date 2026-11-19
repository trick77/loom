package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigration0063_ArtifactsFollowTheirThreadsProject re-applies the real
// migration file to rows written before a thread move carried its artifacts:
// an artifact still naming the project its thread left is cascaded by that
// project's delete while the thread lives on.
func TestMigration0063_ArtifactsFollowTheirThreadsProject(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, oidc_subject, username, role) VALUES ('u1','s1','alice','user')`)
	exec(`INSERT INTO projects (id, user_id, name) VALUES ('pa','u1','A'), ('pb','u1','B')`)
	exec(`INSERT INTO threads (id, user_id, title, project_id) VALUES ('t_b','u1','moved to B','pb')`)
	exec(`INSERT INTO threads (id, user_id, title, project_id) VALUES ('t_none','u1','moved out', NULL)`)
	for _, row := range [][]any{
		{"stale_b", "t_b", "pa"},       // made in A, thread now in B
		{"stale_none", "t_none", "pa"}, // made in A, thread now in no project
		{"upload", "t_b", nil},         // composer upload: follows its thread, no project
		{"knowledge", "t_b", "pa"},     // backs A's knowledge: A's
		{"project_upload", nil, "pa"},  // project upload, no thread
	} {
		exec(`INSERT INTO artifacts (id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes)
VALUES (?, 'u1', ?, ?, 'f', 'files/f', 'text/plain', 1)`, row...)
	}
	exec(`INSERT INTO documents (id, user_id, project_id, artifact_id, volume_relpath, filename, mime, size_bytes)
VALUES ('d1','u1','pa','knowledge','files/f','f','text/plain',1)`)

	body, err := migrationsFS.ReadFile("migrations/0063_artifacts_follow_thread_project.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("apply 0063 against populated db: %v", err)
	}

	want := map[string]string{
		"stale_b":        "pb",
		"stale_none":     "",
		"upload":         "",
		"knowledge":      "pa",
		"project_upload": "pa",
	}
	for id, project := range want {
		var got sql.NullString
		if err := db.QueryRow(`SELECT project_id FROM artifacts WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("select %s: %v", id, err)
		}
		if got.String != project {
			t.Errorf("artifact %s project_id = %q, want %q", id, got.String, project)
		}
	}
}
