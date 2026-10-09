package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
)

// TestImageRoutingFor covers the pure mapping from a semantic ImageIntent plus
// the two image-presence flags to the concrete routing decision. The language
// understanding that produces the intent lives in the gate's prompt (exercised
// by the llm package's ClassifyImageIntent test and the end-to-end run), so this
// test stays deterministic and network-free.
func TestImageRoutingFor(t *testing.T) {
	create := llm.ImageIntent{Action: llm.ImageIntentCreate}
	createText := llm.ImageIntent{Action: llm.ImageIntentCreate, NeedsText: true}
	edit := llm.ImageIntent{Action: llm.ImageIntentEdit}
	editText := llm.ImageIntent{Action: llm.ImageIntentEdit, NeedsText: true}
	none := llm.ImageIntent{Action: llm.ImageIntentNone}

	tests := []struct {
		name         string
		intent       llm.ImageIntent
		attached     bool
		threadHasImg bool
		want         imageRouting
	}{
		// A create always generates; typography follows needs_text and does not
		// depend on an image being present.
		{"create fresh", create, false, false, imageRouting{generate: true}},
		{"create logo -> typography", createText, false, false, imageRouting{generate: true, typography: true}},
		{"create ignores prior image", create, false, true, imageRouting{generate: true}},

		// An edit routes only when a source image exists; reuseSource fires only for
		// the prior-image case (no fresh attachment to act on instead).
		{"edit, no image -> nothing", edit, false, false, imageRouting{}},
		{"edit, prior image -> reuse", edit, false, true, imageRouting{generate: true, reuseSource: true}},
		{"edit, attachment -> no reuse", edit, true, false, imageRouting{generate: true}},
		{"edit, both -> attachment wins, no reuse", edit, true, true, imageRouting{generate: true}},
		{"edit typography, prior image", editText, false, true, imageRouting{generate: true, reuseSource: true, typography: true}},
		// needs_text is meaningless without a source image to act on.
		{"edit typography, no image -> nothing", editText, false, false, imageRouting{}},

		// none never routes.
		{"none", none, false, false, imageRouting{}},
		{"none with attachment", none, true, false, imageRouting{}},
		{"none with prior image", none, false, true, imageRouting{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := imageRoutingFor(tt.intent, tt.attached, tt.threadHasImg); got != tt.want {
				t.Fatalf("imageRoutingFor(%+v, attached=%v, thread=%v) = %+v, want %+v",
					tt.intent, tt.attached, tt.threadHasImg, got, tt.want)
			}
		})
	}
}

// TestClassifyImageTurnPreconditions checks the cheap short-circuits that must
// run before (and instead of) the gate: no image tooling configured, and an
// empty message both skip the LLM call and route as non-image.
func TestClassifyImageTurnPreconditions(t *testing.T) {
	create := llm.ImageIntent{Action: llm.ImageIntentCreate}

	// Image tooling configured + a non-empty message: the gate's intent is mapped.
	configured := &Engine{
		artifacts:  fakeArtifactStore{},
		usersDir:   t.TempDir(),
		imageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})},
		llm:        fakeChatClient{ImageIntent: create},
	}
	if got := configured.classifyImageTurn(context.Background(), llm.InferenceMetadata{UserID: "user_1", Username: "jan", ThreadID: "thr_1"}, "zeichne mir einen Fuchs", false, nil); !got.generate {
		t.Fatalf("classifyImageTurn(create intent) = %+v, want generate=true", got)
	}
	// Empty message never routes, even with tooling and a create-returning gate.
	if got := configured.classifyImageTurn(context.Background(), llm.InferenceMetadata{UserID: "user_1", Username: "jan", ThreadID: "thr_1"}, "   ", false, nil); got != (imageRouting{}) {
		t.Fatalf("classifyImageTurn(empty content) = %+v, want zero routing", got)
	}

	// No image tooling: never routes (and the gate must not be consulted — a nil
	// llm would panic if it were).
	noTools := &Engine{artifacts: fakeArtifactStore{}, usersDir: t.TempDir()}
	if got := noTools.classifyImageTurn(context.Background(), llm.InferenceMetadata{UserID: "user_1", Username: "jan", ThreadID: "thr_1"}, "zeichne mir einen Fuchs", true, nil); got != (imageRouting{}) {
		t.Fatalf("classifyImageTurn(no image tools) = %+v, want zero routing", got)
	}
}

func TestLoadEditSourceImageScopesAndValidates(t *testing.T) {
	usersDir := t.TempDir()
	userID := "user-1"
	// Write a real PNG for the in-scope artifact so ResolveExisting + ReadFile succeed.
	rel := "files/photo.png"
	abs := filepath.Join(usersDir, userID, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	png := []byte("\x89PNG\r\n\x1a\nthe-original-bytes")
	if err := os.WriteFile(abs, png, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	store := fakeArtifactStore{Artifacts: []artifact.Artifact{
		{ID: "img_ok", UserID: userID, ThreadID: "thr_1", VolumeRelPath: rel, MIMEType: "image/png"},
		{ID: "img_other_thread", UserID: userID, ThreadID: "thr_2", VolumeRelPath: rel, MIMEType: "image/png"},
		{ID: "img_bad_mime", UserID: userID, ThreadID: "thr_1", VolumeRelPath: rel, MIMEType: "image/bmp"},
	}}
	srv := &Engine{artifacts: store, usersDir: usersDir}

	// Happy path: original bytes are returned for an in-scope, allowed image.
	src, ok, err := srv.loadEditSourceImage(context.Background(), userID, "img_ok")
	if err != nil || !ok {
		t.Fatalf("loadEditSourceImage(img_ok) = ok %v, err %v", ok, err)
	}
	if !bytes.Equal(src.Data, png) {
		t.Fatalf("Data = %q, want original bytes", src.Data)
	}

	// An image from another of the user's threads is a valid edit source: "Use
	// in thread" re-references it from a new thread, and the vision path already
	// accepts it, so the edit path must too or the follow-up edit silently loses
	// its source. Ownership (the user-scoped lookup) is the real boundary.
	if _, ok, err := srv.loadEditSourceImage(context.Background(), userID, "img_other_thread"); err != nil || !ok {
		t.Fatalf("loadEditSourceImage(img_other_thread) = ok %v, err %v, want ok=true", ok, err)
	}

	// Unsupported MIME, missing, and empty id all degrade to ok=false without an
	// error so the turn proceeds prompt-only.
	for _, id := range []string{"img_bad_mime", "img_missing", ""} {
		_, ok, err := srv.loadEditSourceImage(context.Background(), userID, id)
		if err != nil || ok {
			t.Fatalf("loadEditSourceImage(%q) = ok %v, err %v, want ok=false, err=nil", id, ok, err)
		}
	}
}

func TestLatestImageArtifactIDReturnsNewestWithID(t *testing.T) {
	messages := []chat.Message{
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_old","mimeType":"image/png"}]`)},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"doc_1","mimeType":"application/pdf"}]`)},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_new","mimeType":"image/jpeg"}]`)},
	}
	if got := latestImageArtifactID(messages); got != "img_new" {
		t.Fatalf("latestImageArtifactID = %q, want img_new", got)
	}

	// No image artifacts, or image artifacts missing an id, yield "" — there is
	// nothing to silently re-attach as the model's vision input.
	none := []chat.Message{
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"doc_1","mimeType":"text/plain"}]`)},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"mimeType":"image/png"}]`)},
	}
	if got := latestImageArtifactID(none); got != "" {
		t.Fatalf("latestImageArtifactID = %q, want empty", got)
	}
}
