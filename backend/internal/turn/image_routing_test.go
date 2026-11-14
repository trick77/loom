package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/turn/turntest"
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
		artifacts: fakeArtifactStore{},
		usersDir:  t.TempDir(),
		mcp:       turntest.ImageService(t, fakeImageProvider{}, "", ""),
		llm:       fakeChatClient{ImageIntent: create},
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
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_old","mimeType":"image/png","generated":true}]`)},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"doc_1","mimeType":"application/pdf"}]`)},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_new","mimeType":"image/jpeg","generated":true}]`)},
	}
	if got := latestImageArtifactID(messages); got != "img_new" {
		t.Fatalf("latestImageArtifactID = %q, want img_new", got)
	}

	// An image the image tool kept before the generated flag names its model.
	legacy := []chat.Message{{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_kept","mimeType":"image/png","model":"m1"}]`)}}
	if got := latestImageArtifactID(legacy); got != "img_kept" {
		t.Fatalf("latestImageArtifactID = %q, want img_kept", got)
	}
	// Another tool's image that reports a model is no edit source today.
	other := []chat.Message{{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"shot","mimeType":"image/png","model":"m1","generated":false}]`)}}
	if got := latestImageArtifactID(other); got != "" {
		t.Fatalf("latestImageArtifactID = %q, want empty", got)
	}

	// No image artifacts, or image artifacts missing an id, yield "" — there is
	// nothing to silently re-attach as the model's vision input.
	none := []chat.Message{
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"doc_1","mimeType":"text/plain"}]`)},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"mimeType":"image/png","generated":true}]`)},
	}
	if got := latestImageArtifactID(none); got != "" {
		t.Fatalf("latestImageArtifactID = %q, want empty", got)
	}
}

// A Strava analysis thread holds only run_python charts: none is an edit
// source, so a correction of the answer never reaches the image model with a
// chart. Charts after a generated image hide it too: an edit then means the
// chart in view, never a picture from many turns back.
func TestLatestImageArtifactIDSkipsToolCharts(t *testing.T) {
	charts := []chat.Message{
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"chart_1","mimeType":"image/png","displayFilename":"load_profile_1.png"},` +
			`{"id":"chart_2","mimeType":"image/png","displayFilename":"split_variation_1.png"}]`)},
		{Role: chat.RoleUser},
		{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"chart_3","mimeType":"image/png"}]`)},
	}
	if got := latestImageArtifactID(charts); got != "" {
		t.Fatalf("latestImageArtifactID over charts = %q, want empty", got)
	}

	generated := chat.Message{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_fox","mimeType":"image/png","generated":true}]`)}
	if got := latestImageArtifactID(append([]chat.Message{generated}, charts...)); got != "" {
		t.Fatalf("latestImageArtifactID = %q, want empty (charts on top of the fox)", got)
	}
	if got := latestImageArtifactID(append(slices.Clone(charts), generated)); got != "img_fox" {
		t.Fatalf("latestImageArtifactID = %q, want img_fox (generated after the charts)", got)
	}
}

// flagRecordingChatClient answers the image gate with intent and keeps the
// thread-has-image flag it was given.
type flagRecordingChatClient struct {
	fakeChatClient
	intent         llm.ImageIntent
	threadHasImage bool
}

func (f *flagRecordingChatClient) ClassifyImageIntent(_ context.Context, _ string, _, threadHasImage bool) (llm.ImageIntent, error) {
	f.threadHasImage = threadHasImage
	return f.intent, nil
}

// The gate hears of an image only when the newest one is the image tool's,
// and the edit reuses exactly that one.
func TestClassifyImageTurnSourceFromGeneratedImage(t *testing.T) {
	charts := []chat.Message{{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"chart_1","mimeType":"image/png"}]`)}}
	fox := []chat.Message{{Role: chat.RoleAssistant, Artifacts: json.RawMessage(`[{"id":"img_fox","mimeType":"image/png","generated":true}]`)}}
	llmClient := &flagRecordingChatClient{intent: llm.ImageIntent{Action: llm.ImageIntentEdit}}
	e := &Engine{
		artifacts: fakeArtifactStore{},
		usersDir:  t.TempDir(),
		mcp:       turntest.ImageService(t, fakeImageProvider{}, "", ""),
		llm:       llmClient,
	}
	inf := llm.InferenceMetadata{UserID: "user_1", Username: "jan", ThreadID: "thr_1"}

	if got := e.classifyImageTurn(context.Background(), inf, "you got it wrong, it was after lunch", false, charts); got.generate || got.sourceID != "" {
		t.Fatalf("over charts: %+v, want no image route", got)
	}
	if llmClient.threadHasImage {
		t.Fatal("the gate was told a chart thread has an image")
	}
	if got := e.classifyImageTurn(context.Background(), inf, "make it cyberpunk", false, fox); !got.reuseSource || got.sourceID != "img_fox" {
		t.Fatalf("over a generated image: %+v, want reuse of img_fox", got)
	}
	if !llmClient.threadHasImage {
		t.Fatal("the gate was not told about the generated image")
	}
}
