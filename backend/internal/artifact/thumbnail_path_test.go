package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

// The thumbnail subtree had no symlink check at all, and WriteThumbnail
// creates directories through it.
func TestResolveThumbnailExistingRejectsSymlinkEscape(t *testing.T) {
	usersDir := t.TempDir()
	loomDir := filepath.Join(usersDir, "user_1", ".loom")
	if err := os.MkdirAll(loomDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(loomDir, "thumbnails")); err != nil {
		t.Fatal(err)
	}

	if _, err := ResolveThumbnailExisting(usersDir, "user_1", ".loom/thumbnails/files/x.png.jpg"); err == nil {
		t.Fatal("ResolveThumbnailExisting() error = nil, want symlinked thumbnail dir rejected")
	}
}

func TestResolveThumbnailExistingAcceptsMissingPathInsideRoot(t *testing.T) {
	usersDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(usersDir, "user_1"), 0o700); err != nil {
		t.Fatal(err)
	}

	abs, err := ResolveThumbnailExisting(usersDir, "user_1", ".loom/thumbnails/files/x.png.jpg")
	if err != nil {
		t.Fatalf("ResolveThumbnailExisting() error = %v, want nil for a not-yet-written thumbnail", err)
	}
	if !filepath.IsAbs(abs) {
		t.Fatalf("ResolveThumbnailExisting() = %q, want absolute", abs)
	}
}
