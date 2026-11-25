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

// The tile-sized thumbnail lives apart from the older, smaller one, so a stored
// path tells which size is on disk; a delete removes both.
func TestThumbnailSizesLiveApartAndRemoveTakesBoth(t *testing.T) {
	const rel = "files/outputs/robot.png"
	current, legacy := ThumbnailRelPath(rel), legacyThumbnailRelPath(rel)
	if current != ".loom/thumbnails/v2/files/outputs/robot.png.jpg" {
		t.Fatalf("ThumbnailRelPath() = %q", current)
	}
	if legacy != ".loom/thumbnails/files/outputs/robot.png.jpg" {
		t.Fatalf("legacyThumbnailRelPath() = %q", legacy)
	}
	if !IsCurrentThumbnail(rel, current) || IsCurrentThumbnail(rel, legacy) || IsCurrentThumbnail(rel, "") {
		t.Fatal("IsCurrentThumbnail() must accept only the current path")
	}

	usersDir := t.TempDir()
	for _, thumb := range []string{current, legacy} {
		abs := filepath.Join(usersDir, "user_1", filepath.FromSlash(thumb))
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("jpg"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	RemoveThumbnail(usersDir, "user_1", rel)
	for _, thumb := range []string{current, legacy} {
		if _, err := os.Stat(filepath.Join(usersDir, "user_1", filepath.FromSlash(thumb))); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after RemoveThumbnail, err = %v", thumb, err)
		}
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
