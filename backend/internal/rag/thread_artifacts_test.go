package rag

import (
	"context"
	"testing"
)

// seedThreadArtifactFixture builds the shape the thread-delete path has to tell
// apart: one thread-private document (its own upload) and one project knowledge
// document uploaded from that same thread, so its artifact carries the thread id
// as provenance only.
func seedThreadArtifactFixture(t *testing.T, s *Store) {
	t.Helper()
	db := s.db
	if _, err := db.Exec(`INSERT INTO threads (id, user_id, title) VALUES ('t1','u1','T1'), ('t2','u1','T2')`); err != nil {
		t.Fatalf("seed threads: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO artifacts (id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source)
VALUES ('a_private','u1','t1',NULL,'private.txt','files/private.txt','text/plain',10,'user_uploaded'),
       ('a_project','u1','t1','p1','shared.txt','projects/p1/shared.txt','text/plain',10,'user_uploaded'),
       ('a_generated','u1','t1',NULL,'chart.png','files/outputs/chart.png','image/png',10,'assistant_generated')`); err != nil {
		t.Fatalf("seed artifacts: %v", err)
	}
	ctx := context.Background()
	threadID := "t1"
	privateArtifact, projectArtifact := "a_private", "a_project"
	projectID := "p1"
	docs := []Document{
		{ID: "d_private", UserID: "u1", ThreadID: &threadID, ArtifactID: &privateArtifact, VolumeRelpath: "files/private.txt", Filename: "private.txt", MIME: "text/plain", SizeBytes: 10, Status: StatusEmbedded},
		{ID: "d_project", UserID: "u1", ProjectID: &projectID, ArtifactID: &projectArtifact, VolumeRelpath: "projects/p1/shared.txt", Filename: "shared.txt", MIME: "text/plain", SizeBytes: 10, Status: StatusEmbedded},
	}
	for _, doc := range docs {
		if err := s.CreateDocument(ctx, doc); err != nil {
			t.Fatalf("CreateDocument %s: %v", doc.ID, err)
		}
	}
}

func TestStore_ArtifactIDsForThreadArtifactsInUse(t *testing.T) {
	s, _ := newTestStore(t)
	seedThreadArtifactFixture(t, s)
	ctx := context.Background()

	// Called before the thread's own documents go, both backed artifacts match.
	before, err := s.ArtifactIDsForThreadArtifactsInUse(ctx, "u1", "t1")
	if err != nil {
		t.Fatalf("ArtifactIDsForThreadArtifactsInUse: %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("before thread cleanup = %v, want both backed artifacts", before)
	}

	// In the real order the thread's private documents are deleted first, leaving
	// only the project document's artifact to spare. The generated artifact backs
	// no document and must never appear.
	if err := s.DeleteThreadScopeDocuments(ctx, "u1", "t1"); err != nil {
		t.Fatalf("DeleteThreadScopeDocuments: %v", err)
	}
	got, err := s.ArtifactIDsForThreadArtifactsInUse(ctx, "u1", "t1")
	if err != nil {
		t.Fatalf("ArtifactIDsForThreadArtifactsInUse: %v", err)
	}
	if len(got) != 1 || got[0] != "a_project" {
		t.Fatalf("in-use artifacts = %v, want [a_project]", got)
	}

	// Scoping: another user's id and an unrelated thread both come back empty.
	if other, err := s.ArtifactIDsForThreadArtifactsInUse(ctx, "u2", "t1"); err != nil || len(other) != 0 {
		t.Fatalf("u2 in-use artifacts = %v (err %v), want none", other, err)
	}
	if other, err := s.ArtifactIDsForThreadArtifactsInUse(ctx, "u1", "t2"); err != nil || len(other) != 0 {
		t.Fatalf("t2 in-use artifacts = %v (err %v), want none", other, err)
	}
}
