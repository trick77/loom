package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateUploadFileGlobalScope(t *testing.T) {
	root := t.TempDir()
	out, f, err := CreateUploadFile(UploadRequest{
		UsersDir:        root,
		UserID:          "user_1",
		DisplayFilename: "My Notes.pdf",
		Extension:       "pdf",
	})
	if err != nil {
		t.Fatalf("CreateUploadFile() error = %v", err)
	}
	defer f.Close()
	if out.VolumeRelPath != "files/My Notes.pdf" {
		t.Fatalf("VolumeRelPath = %q, want files/My Notes.pdf", out.VolumeRelPath)
	}
	if filepath.Dir(out.AbsPath) != filepath.Join(root, "user_1", "files") {
		t.Fatalf("AbsPath dir = %q", filepath.Dir(out.AbsPath))
	}
	if _, err := os.Stat(out.AbsPath); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestCreateUploadFileProjectScope(t *testing.T) {
	root := t.TempDir()
	out, f, err := CreateUploadFile(UploadRequest{
		UsersDir:        root,
		UserID:          "user_1",
		ProjectID:       strPtr("proj_1"),
		DisplayFilename: "data.csv",
		Extension:       "csv",
	})
	if err != nil {
		t.Fatalf("CreateUploadFile() error = %v", err)
	}
	defer f.Close()
	if out.VolumeRelPath != "projects/proj_1/data.csv" {
		t.Fatalf("VolumeRelPath = %q, want projects/proj_1/data.csv", out.VolumeRelPath)
	}
}

func TestCreateUploadFileRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	// A project id attempting traversal must be rejected by the sandbox.
	if _, _, err := CreateUploadFile(UploadRequest{
		UsersDir:        root,
		UserID:          "user_1",
		ProjectID:       strPtr("../../etc"),
		DisplayFilename: "x.txt",
		Extension:       "txt",
	}); err == nil {
		t.Fatal("CreateUploadFile() error = nil, want rejection for traversal project id")
	}
}
