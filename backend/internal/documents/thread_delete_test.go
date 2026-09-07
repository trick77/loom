package documents

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/store"
)

// newThreadDeleteFixture builds the two kinds of upload a project thread can hold
// and returns the pieces the thread-delete path works with.
func newThreadDeleteFixture(t *testing.T) (*Service, *artifact.Store, *sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "db.sqlite"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
INSERT INTO users (id, oidc_subject, username, role) VALUES ('u','s','u','user');
INSERT INTO projects (id, user_id, name) VALUES ('prj_1','u','Project');
INSERT INTO threads (id, user_id, project_id, title) VALUES ('thread_1','u','prj_1','Thread');`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	usersDir := filepath.Join(dir, "users")
	artifacts := artifact.NewStore(db)
	svc := NewService(rag.NewStore(db), artifacts, &fakeIndexer{}, fakeEmbedder{}, usersDir)
	return svc, artifacts, db, usersDir
}

// Deleting a thread must take its own private uploads and leave the project
// knowledge uploaded from it. Both documents' artifacts carry the thread id, so
// the cascade alone cannot tell them apart, and letting it reach the project
// document's artifact does not merely orphan that document: the artifacts
// cascade fires documents' ON DELETE SET NULL over the composite (user_id,
// artifact_id) key, which nulls user_id too and aborts the whole delete on its
// NOT NULL constraint. Detaching first is what keeps the thread deletable.
//
// Drop the DetachFromThread call below and this test fails with
// "NOT NULL constraint failed: documents.user_id".
func TestDeleteThreadKeepsProjectDocumentUploadedFromThread(t *testing.T) {
	svc, artifacts, db, usersDir := newThreadDeleteFixture(t)
	ctx := context.Background()
	projectID := "prj_1"

	projectDoc, projectArt, err := svc.Upload(ctx, UploadInput{
		UserID: "u", ThreadID: "thread_1", ProjectID: &projectID,
		Filename: "shared.txt", Reader: strings.NewReader("shared"),
	})
	if err != nil {
		t.Fatalf("upload project document: %v", err)
	}
	privateDoc, privateArt, err := svc.Upload(ctx, UploadInput{
		UserID: "u", ThreadID: "thread_1",
		Filename: "private.txt", Reader: strings.NewReader("private"),
	})
	if err != nil {
		t.Fatalf("upload thread document: %v", err)
	}
	projectFile := filepath.Join(usersDir, "u", filepath.FromSlash(projectDoc.VolumeRelpath))
	privateFile := filepath.Join(usersDir, "u", filepath.FromSlash(privateDoc.VolumeRelpath))

	// The delete path, in order: private knowledge first, then spare the artifacts
	// still backing a surviving document, then let the cascade take the rest.
	if err := svc.DeleteThreadData(ctx, "u", "thread_1"); err != nil {
		t.Fatalf("DeleteThreadData: %v", err)
	}
	inUse, err := svc.ArtifactIDsForThreadArtifactsInUse(ctx, "u", "thread_1")
	if err != nil {
		t.Fatalf("ArtifactIDsForThreadArtifactsInUse: %v", err)
	}
	if len(inUse) != 1 || inUse[0] != projectArt.ID {
		t.Fatalf("in-use artifacts = %v, want [%s]", inUse, projectArt.ID)
	}
	if err := artifacts.DetachFromThread(ctx, "u", inUse); err != nil {
		t.Fatalf("DetachFromThread: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM threads WHERE user_id = 'u' AND id = 'thread_1'`); err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	// Files are unlinked after the delete, sparing the in-use ones.
	if err := os.Remove(privateFile); err != nil {
		t.Fatalf("remove thread-owned file: %v", err)
	}

	// The project document survives whole: row, artifact link, and bytes.
	got, ok, err := svc.Get(ctx, "u", projectDoc.ID)
	if err != nil || !ok {
		t.Fatalf("project document ok=%v err=%v, want it to survive", ok, err)
	}
	if got.ArtifactID == nil || *got.ArtifactID != projectArt.ID {
		t.Errorf("project document ArtifactID = %v, want %q", got.ArtifactID, projectArt.ID)
	}
	if _, ok, err := artifacts.Get(ctx, "u", projectArt.ID); err != nil || !ok {
		t.Errorf("project artifact ok=%v err=%v, want it to survive the cascade", ok, err)
	}
	if _, err := os.Stat(projectFile); err != nil {
		t.Errorf("project document file: %v, want it to still exist", err)
	}

	// The thread's own upload is gone: document row, artifact row, and file.
	if _, ok, _ := svc.Get(ctx, "u", privateDoc.ID); ok {
		t.Error("thread-private document survived the delete")
	}
	if _, ok, _ := artifacts.Get(ctx, "u", privateArt.ID); ok {
		t.Error("thread-private artifact survived the cascade")
	}
	if _, err := os.Stat(privateFile); !os.IsNotExist(err) {
		t.Errorf("thread-private file stat = %v, want not-exist", err)
	}
}
