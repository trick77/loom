package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveOutputPathUsesThreadScope(t *testing.T) {
	root := t.TempDir()
	req := OutputRequest{
		UsersDir:        root,
		UserID:          "user_1",
		ThreadID:        "thread_1",
		ProjectID:       strPtr("proj_1"),
		DisplayFilename: "Q1 report.pdf",
		Extension:       "pdf",
	}

	out, err := ResolveOutputPath(req)
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if out.VolumeRelPath != "projects/proj_1/outputs/Q1 report.pdf" {
		t.Fatalf("VolumeRelPath = %q", out.VolumeRelPath)
	}
	if filepath.Dir(out.AbsPath) != filepath.Join(root, "user_1", "projects", "proj_1", "outputs") {
		t.Fatalf("AbsPath directory = %q", filepath.Dir(out.AbsPath))
	}
}

func TestResolveOutputPathUsesProjectlessOutputs(t *testing.T) {
	root := t.TempDir()
	out, err := ResolveOutputPath(OutputRequest{
		UsersDir:        root,
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "notes.md",
		Extension:       "md",
	})
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if out.VolumeRelPath != "files/outputs/notes.md" {
		t.Fatalf("VolumeRelPath = %q", out.VolumeRelPath)
	}
}

func TestResolveOutputPathRejectsTraversalAndReservedPaths(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../secret.pdf", "/tmp/secret.pdf", ".loom/secret.pdf", "folder/../../secret.pdf"} {
		_, err := ResolveOutputPath(OutputRequest{
			UsersDir:        root,
			UserID:          "user_1",
			ThreadID:        "thread_1",
			DisplayFilename: name,
			Extension:       "pdf",
		})
		if err == nil {
			t.Fatalf("ResolveOutputPath(%q) succeeded, want error", name)
		}
	}
}

func TestResolveOutputPathAddsCollisionSuffix(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "user_1", "files", "outputs")
	if err := os.MkdirAll(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "report.pdf"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := ResolveOutputPath(OutputRequest{
		UsersDir:        root,
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "report.pdf",
		Extension:       "pdf",
	})
	if err != nil {
		t.Fatalf("ResolveOutputPath() error = %v", err)
	}
	if out.DisplayFilename != "report-2.pdf" {
		t.Fatalf("DisplayFilename = %q", out.DisplayFilename)
	}
}

func TestCreateOutputFileAddsCollisionSuffixAtomically(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "user_1", "files", "outputs")
	if err := os.MkdirAll(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "report.pdf"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, file, err := CreateOutputFile(OutputRequest{
		UsersDir:        root,
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "report.pdf",
		Extension:       "pdf",
	})
	if err != nil {
		t.Fatalf("CreateOutputFile() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if out.DisplayFilename != "report-2.pdf" {
		t.Fatalf("DisplayFilename = %q", out.DisplayFilename)
	}
	if _, err := os.Stat(filepath.Join(existing, "report-2.pdf")); err != nil {
		t.Fatalf("created file missing: %v", err)
	}
}

func TestResolveOutputPathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outputs := filepath.Join(root, "user_1", "files", "outputs")
	if err := os.MkdirAll(outputs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(outputs, "escape")); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveExisting(root, "user_1", "files/outputs/escape/report.pdf")
	if err == nil {
		t.Fatal("ResolveExisting through symlink succeeded, want error")
	}
}

func TestCreateOutputFileRejectsSymlinkOutputDirectory(t *testing.T) {
	root := t.TempDir()
	filesDir := filepath.Join(root, "user_1", "files")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(filesDir, "outputs")); err != nil {
		t.Fatal(err)
	}

	_, _, err := CreateOutputFile(OutputRequest{
		UsersDir:        root,
		UserID:          "user_1",
		ThreadID:        "thread_1",
		DisplayFilename: "report.pdf",
		Extension:       "pdf",
	})
	if err == nil {
		t.Fatal("CreateOutputFile through symlink output dir succeeded, want error")
	}
}

func TestMIMETypeImages(t *testing.T) {
	tests := map[string]string{
		"png":  "image/png",
		".jpg": "image/jpeg",
		"jpeg": "image/jpeg",
		"webp": "image/webp",
		"svg":  "image/svg+xml; charset=utf-8",
	}
	for extension, want := range tests {
		if got := MIMEType(extension); got != want {
			t.Fatalf("MIMEType(%q) = %q, want %q", extension, got, want)
		}
	}
}

func strPtr(value string) *string {
	return &value
}

// The symlink check covered the parent directory only: a symlinked *file*
// inside the sandbox was resolved by os.Open to wherever it pointed.
func TestResolveExistingRejectsSymlinkedFile(t *testing.T) {
	usersDir := t.TempDir()
	outputs := filepath.Join(usersDir, "user_1", "files", "outputs")
	if err := os.MkdirAll(outputs, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.pdf")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(outputs, "report.pdf")); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveExisting(usersDir, "user_1", "files/outputs/report.pdf"); err == nil {
		t.Fatal("ResolveExisting() error = nil, want symlinked file rejected")
	}
}

// ".." as a substring is not traversal: a file called a..b.pdf is legitimate.
func TestResolveExistingAllowsDoubleDotInName(t *testing.T) {
	usersDir := t.TempDir()
	outputs := filepath.Join(usersDir, "user_1", "files", "outputs")
	if err := os.MkdirAll(outputs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputs, "a..b.pdf"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	abs, err := ResolveExisting(usersDir, "user_1", "files/outputs/a..b.pdf")
	if err != nil {
		t.Fatalf("ResolveExisting() error = %v, want nil", err)
	}
	if filepath.Base(abs) != "a..b.pdf" {
		t.Fatalf("ResolveExisting() = %q, want the file itself", abs)
	}
}

func TestResolveExistingRejectsTraversalAndReservedInAnySpelling(t *testing.T) {
	usersDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(usersDir, "user_1"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"../user_2/files/x.pdf",
		"files/../../user_2/x.pdf",
		"./.loom/thumbnails/x.jpg",
		".loom/thumbnails/x.jpg",
		"/etc/passwd",
		"",
		".",
	} {
		if _, err := ResolveExisting(usersDir, "user_1", rel); err == nil {
			t.Errorf("ResolveExisting(%q) error = nil, want rejection", rel)
		}
	}
}

// A project id is a path segment; the output path must apply the same
// single-segment rule the upload path already enforces.
func TestCreateOutputFileRejectsProjectIDWithSeparator(t *testing.T) {
	for _, projectID := range []string{"../x", "a/b", `a\b`, ".."} {
		id := projectID
		_, _, err := CreateOutputFile(OutputRequest{
			UsersDir:        t.TempDir(),
			UserID:          "user_1",
			ProjectID:       &id,
			DisplayFilename: "out.txt",
			Extension:       "txt",
		})
		if err == nil {
			t.Errorf("CreateOutputFile(project %q) error = nil, want invalid project id", projectID)
		}
	}
}
