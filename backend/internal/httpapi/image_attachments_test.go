package httpapi

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
)

// A real artifact keeps the thread it was generated/uploaded in. "Use in thread"
// re-references it from a brand-new thread, so imageContentParts must accept an
// artifact whose ThreadID differs from the request thread — ownership (enforced by
// the user-scoped store) is the real boundary. Regression for the bug where
// cross-thread reuse returned 400 and the model never saw the image even though it
// displayed in the sent bubble.
func TestImageContentParts_acceptsArtifactFromDifferentThread(t *testing.T) {
	// Resolve symlinks: on macOS t.TempDir() lives under /var -> /private/var, and
	// ResolveExisting's EvalSymlinks-based containment check would otherwise flag a
	// spurious "path escapes user root". The real users dir isn't symlinked.
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	const userID = "u1"
	// Real artifacts always live in a subdirectory of the user root (e.g. outputs/),
	// never directly in it; mirror that so ResolveExisting's containment check passes.
	const rel = "outputs/seed-robot.png"
	if err := os.MkdirAll(filepath.Join(usersDir, userID, "outputs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	png, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	)
	if err := os.WriteFile(filepath.Join(usersDir, userID, rel), png, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}

	s := &server{
		usersDir: usersDir,
		artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{{
			ID:              "art_1",
			UserID:          userID,
			ThreadID:        "old-thread",
			DisplayFilename: "seed-robot.png",
			MIMEType:        "image/png",
			VolumeRelPath:   rel,
		}}},
	}

	parts, err := s.imageContentParts(context.Background(), userID, "new-thread", "describe this", []string{"art_1"})
	if err != nil {
		t.Fatalf("cross-thread image attachment must be accepted, got error: %v", err)
	}
	// One image_url part followed by the trailing prompt text part.
	if len(parts) != 2 {
		t.Fatalf("got %d parts, want 2: %+v", len(parts), parts)
	}
	if parts[0].Type != "image_url" || parts[0].ImageURL == nil || !strings.HasPrefix(parts[0].ImageURL.URL, "data:image/") {
		t.Fatalf("first part should be an image data URL, got %+v", parts[0])
	}
	if parts[1].Type != "text" || parts[1].Text != "describe this" {
		t.Fatalf("second part should be the prompt text, got %+v", parts[1])
	}
}

// A forged id pointing at another user's artifact must still be rejected — removing
// the thread-scope check must not weaken the ownership boundary. This is a real
// negative control: the image file genuinely exists at the path the caller ("u1")
// would resolve, so ownership (UserID "someone_else") is the ONLY thing that can
// make it fail. If the user-scoped Get were ever bypassed, the call would succeed
// and this test would fail — which is exactly what a boundary test should do.
func TestImageContentParts_rejectsForeignUserArtifact(t *testing.T) {
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	const caller = "u1"
	const rel = "outputs/secret.png"
	if err := os.MkdirAll(filepath.Join(usersDir, caller, "outputs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	png, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	)
	if err := os.WriteFile(filepath.Join(usersDir, caller, rel), png, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}

	s := &server{
		usersDir: usersDir,
		artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{{
			ID:              "art_1",
			UserID:          "someone_else",
			DisplayFilename: "secret.png",
			MIMEType:        "image/png",
			VolumeRelPath:   rel,
		}}},
	}

	if _, err := s.imageContentParts(context.Background(), caller, "t1", "x", []string{"art_1"}); err == nil {
		t.Fatal("foreign-user artifact must not resolve as an image attachment")
	}
}
