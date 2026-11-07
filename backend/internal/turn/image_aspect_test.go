package turn

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/llm"
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

// The edit path forwards the source pixels to the image tool as input_images;
// the tool keeps their proportions (imagegen's Service), so loom only has to
// hand on the real image.
func TestLoadEditSourceImage_forwardsSourceToImageTool(t *testing.T) {
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
	run := &Run{e: s, plan: turnPlan{editSource: &src}}
	extra := run.loomArgs(llm.ToolCall{Function: llm.ToolCallFunction{Name: imageToolName, Arguments: `{"prompt":"make it a watercolor"}`}})
	images, _ := extra[argInputImages].([]map[string]any)
	if len(images) != 1 || images[0]["mime_type"] != "image/png" {
		t.Fatalf("input_images = %v, want the source PNG", extra[argInputImages])
	}
	data, err := base64.StdEncoding.DecodeString(images[0]["data"].(string))
	if err != nil || !bytes.Equal(data, src.Data) {
		t.Fatalf("input_images data differs from the source (%v)", err)
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
	if !ok || string(src.Data) != "not a png" {
		t.Fatal("an undecodable but in-limit image should still be forwarded")
	}
}
