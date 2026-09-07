package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/store"
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

	s := &server{
		usersDir: usersDir,
		artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{{
			ID:              "art_1",
			UserID:          userID,
			ThreadID:        "t1",
			DisplayFilename: "wide-photo.png",
			MIMEType:        "image/png",
			VolumeRelPath:   rel,
		}}},
	}

	src, ok, err := s.loadEditSourceImage(context.Background(), userID, "t1", "art_1")
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

	s := &server{
		usersDir: usersDir,
		artifacts: fakeArtifactStore{artifacts: []artifact.Artifact{{
			ID:              "art_1",
			UserID:          userID,
			ThreadID:        "t1",
			DisplayFilename: "broken.png",
			MIMEType:        "image/png",
			VolumeRelPath:   rel,
		}}},
	}

	src, ok, err := s.loadEditSourceImage(context.Background(), userID, "t1", "art_1")
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

// End-to-end check that an aspect_ratio the model puts in its tool call survives
// argument parsing, normalization and the typography clamp, and lands on the
// artifact the user sees. The unit tests cover each step; this covers the wiring
// between them, which is where a forgotten field assignment would hide.
func TestStreamMessageAppliesAspectRatioFromToolCall(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO users (id, oidc_subject, username, role)
VALUES ('user_1', 'subject-user_1', 'user_1', 'user')`); err != nil {
		t.Fatal(err)
	}
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	thread, err := threadStore.CreateThread(context.Background(), testUser.ID, chat.CreateThreadInput{Title: "Images"})
	if err != nil {
		t.Fatal(err)
	}

	llmClient := &fakeToolChatClient{
		imageIntent: llm.ImageIntent{Action: llm.ImageIntentCreate},
		results: []llm.StreamResult{{
			ToolCalls: []llm.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "generate_image",
					Arguments: `{"prompt":"a wide desert canyon at dusk","filename":"desert-canyon","aspect_ratio":"16:9"}`,
				},
			}},
		}},
		plain: "Created desert-canyon.png.",
	}
	server := newAuthenticatedServer(t, Deps{
		Thread:     threadStore,
		Artifacts:  artifactStore,
		ImageTools: []imagegen.Tool{imagegen.NewTool(normalizingImageProvider{})},
		UsersDir:   t.TempDir(),
		LLM:        llmClient,
	})

	req := authenticatedRequest(http.MethodPost, "/api/threads/"+thread.ID+"/messages:stream", `{"content":"a wide shot of a desert canyon"}`)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// fakeImageProvider echoes the request dimensions back, and the artifact event
	// carries them, so 1024x576 here proves the ratio was resolved rather than
	// silently defaulting to the 1024x1024 square.
	if !strings.Contains(body, `"width":1024`) || !strings.Contains(body, `"height":576`) {
		t.Fatalf("artifact event does not report a 16:9 size (want 1024x576):\n%s", body)
	}
	if strings.Contains(body, `"height":1024`) {
		t.Fatalf("artifact event reports a square height, aspect_ratio was ignored:\n%s", body)
	}
}

// normalizingImageProvider mirrors what a real provider does with the request it
// is handed: run it through Normalized (which is where aspect_ratio becomes a
// concrete size) and report the size it actually generated. fakeImageProvider
// echoes the raw request instead, so it cannot show that resolution happening.
type normalizingImageProvider struct{}

func (normalizingImageProvider) Generate(_ context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	norm, err := req.Normalized()
	if err != nil {
		return imagegen.GenerateResult{}, err
	}
	return imagegen.GenerateResult{
		Filename:  norm.Filename,
		Extension: "png",
		MIMEType:  "image/png",
		Bytes:     []byte("\x89PNG\r\n\x1a\nfake"),
		Provider:  "fake",
		Model:     "fake-model",
		RequestID: "request-1",
		Prompt:    norm.Prompt,
		Width:     norm.Width,
		Height:    norm.Height,
	}, nil
}
