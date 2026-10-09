package turn

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/imagegen"
)

// writeTestPNG writes a w×h PNG artifact for a user and returns the users dir.
func writeTestPNG(t *testing.T, userID, rel string, w, h int) string {
	t.Helper()
	// Resolve symlinks: on macOS t.TempDir() lives under /var -> /private/var and
	// ResolveExisting's containment check would otherwise flag a spurious escape.
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(usersDir, userID, rel)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if err := os.WriteFile(filepath.Join(usersDir, userID, rel), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	return usersDir
}

// The edit path forwards the source pixels to the model, so the output must keep
// the source's proportions: restyling a 16:9 photo that comes back as a square
// crops or squashes the composition the user asked to preserve.
func TestLoadEditSourceImage_reportsSourceDimensions(t *testing.T) {
	const userID, rel = "u1", "outputs/wide-photo.png"
	usersDir := writeTestPNG(t, userID, rel, 1920, 1080)

	s := &Engine{
		usersDir: usersDir,
		artifacts: fakeArtifactStore{Artifacts: []artifact.Artifact{{
			ID:              "art_1",
			UserID:          userID,
			ThreadID:        "t1",
			DisplayFilename: "wide-photo.png",
			MIMEType:        "image/png",
			VolumeRelPath:   rel,
		}}},
	}

	src, ok, err := s.loadEditSourceImage(context.Background(), userID, "art_1")
	if err != nil || !ok {
		t.Fatalf("loadEditSourceImage() ok=%v err=%v", ok, err)
	}
	if src.Width != 1920 || src.Height != 1080 {
		t.Fatalf("source dimensions = %dx%d, want 1920x1080", src.Width, src.Height)
	}
	// The dispatcher turns those dimensions into the request's aspect ratio.
	ratio := imagegen.AspectRatioForSize(src.Width, src.Height)
	if ratio != "16:9" {
		t.Fatalf("AspectRatioForSize(%d, %d) = %q, want 16:9", src.Width, src.Height, ratio)
	}
	// And that ratio has to survive normalization as a genuinely wide size,
	// not collapse back to the square default.
	req, err := imagegen.GenerateRequest{Prompt: "make it a watercolor", AspectRatio: ratio}.Normalized()
	if err != nil {
		t.Fatalf("Normalized() error = %v", err)
	}
	if req.Width <= req.Height {
		t.Fatalf("edit of a landscape source normalized to %dx%d, want a landscape size", req.Width, req.Height)
	}
}

// An image the decoder cannot read must not fail the edit: the turn degrades to
// the default shape, matching DownscaleForEditInput's best-effort contract.
func TestLoadEditSourceImage_undecodableHasUnknownDimensions(t *testing.T) {
	const userID, rel = "u1", "outputs/broken.png"
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usersDir, userID, "outputs"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(usersDir, userID, rel), []byte("not a png"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	s := &Engine{
		usersDir: usersDir,
		artifacts: fakeArtifactStore{Artifacts: []artifact.Artifact{{
			ID:              "art_1",
			UserID:          userID,
			ThreadID:        "t1",
			DisplayFilename: "broken.png",
			MIMEType:        "image/png",
			VolumeRelPath:   rel,
		}}},
	}

	src, ok, err := s.loadEditSourceImage(context.Background(), userID, "art_1")
	if err != nil {
		t.Fatalf("loadEditSourceImage() error = %v", err)
	}
	if !ok {
		t.Fatal("an undecodable but in-limit image should still be forwarded")
	}
	if src.Width != 0 || src.Height != 0 {
		t.Fatalf("dimensions = %dx%d, want 0x0 for an undecodable image", src.Width, src.Height)
	}
}
