package turn

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// viewImageEngine is an Engine whose thread "t1" holds, oldest first, a chart,
// a CSV and a second chart of user u1, plus one image of another user and one
// of u1 in another thread; every image's bytes exist on disk.
func viewImageEngine(t *testing.T, client *fakeToolChatClient) *Engine {
	t.Helper()
	usersDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	png, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
	)
	write := func(userID, rel string) {
		if err := os.MkdirAll(filepath.Join(usersDir, userID, "outputs"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(usersDir, userID, rel), png, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for _, f := range []struct{ user, rel string }{
		{testUser.ID, "outputs/hr.png"}, {testUser.ID, "outputs/pace.png"}, {testUser.ID, "outputs/other.png"}, {"u2", "outputs/foreign.png"},
	} {
		write(f.user, f.rel)
	}
	return &Engine{
		usersDir: usersDir,
		llm:      client,
		thread:   &fakeThreadStore{},
		artifacts: fakeArtifactStore{Artifacts: []artifact.Artifact{
			{ID: "a1", UserID: testUser.ID, ThreadID: "t1", DisplayFilename: "hr.png", MIMEType: "image/png", VolumeRelPath: "outputs/hr.png"},
			{ID: "a2", UserID: testUser.ID, ThreadID: "t1", DisplayFilename: "splits.csv", MIMEType: "text/csv", VolumeRelPath: "outputs/splits.csv"},
			{ID: "a3", UserID: testUser.ID, ThreadID: "t1", DisplayFilename: "pace.png", MIMEType: "image/png", VolumeRelPath: "outputs/pace.png"},
			{ID: "a4", UserID: "u2", ThreadID: "t1", DisplayFilename: "foreign.png", MIMEType: "image/png", VolumeRelPath: "outputs/foreign.png"},
			{ID: "a5", UserID: testUser.ID, ThreadID: "t2", DisplayFilename: "other.png", MIMEType: "image/png", VolumeRelPath: "outputs/other.png"},
		}},
	}
}

func TestViewImage_namedImageAsksVisionModel(t *testing.T) {
	client := &fakeToolChatClient{ImageAnswer: "The y axis spans 0 to 250 bpm; the line sits between 120 and 160."}
	s := viewImageEngine(t, client)

	out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, map[string]any{"name": "HR.png", "question": "What range does the y axis cover?"})

	if len(client.ImageAsks) != 1 {
		t.Fatalf("AskImage calls = %d, want 1", len(client.ImageAsks))
	}
	ask := client.ImageAsks[0]
	if ask.Question != "What range does the y axis cover?" || ask.MIME != "image/png" || len(ask.Data) == 0 {
		t.Errorf("AskImage got question %q, mime %q, %d bytes", ask.Question, ask.MIME, len(ask.Data))
	}
	for _, want := range []string{"hr.png", "not instructions", "0 to 250 bpm"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// Without a name the newest image of the thread is the one meant: the chart
// just drawn.
func TestViewImage_noNamePicksNewestImage(t *testing.T) {
	client := &fakeToolChatClient{ImageAnswer: "ok"}
	s := viewImageEngine(t, client)

	out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, map[string]any{"question": "Is the curve compressed?"})

	if !strings.HasPrefix(out, "Vision answer about pace.png") {
		t.Fatalf("output does not answer about the newest image pace.png:\n%s", out)
	}
	if !strings.Contains(out, "Other images in this conversation, newest first: hr.png") {
		t.Errorf("output does not name the older image:\n%s", out)
	}
	if len(client.ImageAsks) != 1 {
		t.Fatalf("AskImage calls = %d, want 1", len(client.ImageAsks))
	}
}

// A name not among the thread's own images lists those that are: another
// user's image and one in another thread stay out of reach and out of the list.
func TestViewImage_unknownNameListsThreadImages(t *testing.T) {
	for _, name := range []string{"missing.png", "foreign.png", "other.png", "splits.csv"} {
		t.Run(name, func(t *testing.T) {
			client := &fakeToolChatClient{ImageAnswer: "should not be asked"}
			s := viewImageEngine(t, client)

			out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, map[string]any{"name": name, "question": "q"})

			if len(client.ImageAsks) != 0 {
				t.Fatalf("AskImage called for %q", name)
			}
			if !strings.Contains(out, "pace.png, hr.png") {
				t.Errorf("output does not list the thread's images newest first:\n%s", out)
			}
			for _, leak := range []string{"foreign.png", "other.png", "splits.csv"} {
				if leak != name && strings.Contains(out, leak) {
					t.Errorf("output names %q:\n%s", leak, out)
				}
			}
		})
	}
}

func TestViewImage_threadWithoutImages(t *testing.T) {
	client := &fakeToolChatClient{}
	s := viewImageEngine(t, client)

	out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t-empty"}, map[string]any{"question": "q"})

	if !strings.Contains(out, "no images") || len(client.ImageAsks) != 0 {
		t.Fatalf("want a no-images note and no vision call, got:\n%s", out)
	}
}

func TestViewImage_questionRequiredAndBounded(t *testing.T) {
	for _, question := range []string{"  ", strings.Repeat("ä", maxViewImageQuestionRunes+1)} {
		client := &fakeToolChatClient{}
		s := viewImageEngine(t, client)

		out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, map[string]any{"name": "hr.png", "question": question})

		if !strings.HasPrefix(out, "tool failed:") || len(client.ImageAsks) != 0 {
			t.Fatalf("question of %d runes: want a tool failure without a vision call, got:\n%s", len([]rune(question)), out)
		}
	}
}

// The path the chart was saved to names it too.
func TestViewImage_pathNameMatchesFileName(t *testing.T) {
	client := &fakeToolChatClient{ImageAnswer: "ok"}
	s := viewImageEngine(t, client)

	out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, map[string]any{"name": "out/hr.png", "question": "q"})

	if !strings.HasPrefix(out, "Vision answer about hr.png") {
		t.Fatalf("out/hr.png did not resolve to hr.png:\n%s", out)
	}
}

// Two charts saved under one name stay apart: numbered oldest first, so a
// number keeps naming the same chart; the bare name is the newest.
func TestViewImage_sameNameImagesAreNumbered(t *testing.T) {
	client := &fakeToolChatClient{ImageAnswer: "ok"}
	s := viewImageEngine(t, client)
	store := s.artifacts.(fakeArtifactStore)
	store.Artifacts = append(store.Artifacts,
		artifact.Artifact{ID: "a6", UserID: testUser.ID, ThreadID: "t1", DisplayFilename: "hr.png", MIMEType: "image/png", VolumeRelPath: "outputs/pace.png"})
	s.artifacts = store

	out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, map[string]any{"name": "nope.png", "question": "q"})
	if !strings.Contains(out, "hr.png#2, pace.png, hr.png#1") {
		t.Fatalf("miss does not number the two hr.png oldest first:\n%s", out)
	}
	for name, label := range map[string]string{"hr.png": "hr.png#2", "HR.png#1": "hr.png#1"} {
		out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, map[string]any{"name": name, "question": "q"})
		if !strings.HasPrefix(out, "Vision answer about "+label+" (") {
			t.Fatalf("%s did not resolve to %s:\n%s", name, label, out)
		}
	}
	if got := client.ImageAsks; len(got) != 2 {
		t.Fatalf("AskImage calls = %d, want 2", len(got))
	}
}

// An image the thread's messages carried belongs to the conversation even
// when its artifact lives elsewhere: re-used from another thread ("Use in
// thread") or uploaded to a project chat before the thread existed. A
// deleted one, or one another thread merely holds, stays out.
func TestViewImage_seesImagesAttachedFromElsewhere(t *testing.T) {
	client := &fakeToolChatClient{ImageAnswer: "ok"}
	s := viewImageEngine(t, client)
	project := "p1"
	store := s.artifacts.(fakeArtifactStore)
	store.Artifacts = append(store.Artifacts,
		artifact.Artifact{ID: "reused", UserID: testUser.ID, ThreadID: "t-old", DisplayFilename: "logo.png", MIMEType: "image/png", VolumeRelPath: "outputs/hr.png"},
		artifact.Artifact{ID: "p-up", UserID: testUser.ID, ProjectID: &project, DisplayFilename: "screenshot.png", MIMEType: "image/png", VolumeRelPath: "outputs/hr.png"},
		artifact.Artifact{ID: "gone", UserID: testUser.ID, ThreadID: "t-old", DisplayFilename: "gone.png", MIMEType: "image/png", VolumeRelPath: "outputs/hr.png", Deleted: true})
	s.artifacts = store
	s.thread = &fakeThreadStore{Messages: []chat.Message{
		{ID: "m1", Role: chat.RoleUser, Attachments: []byte(`[{"kind":"image","artifactId":"p-up"},{"kind":"document","documentId":"d1"}]`)},
		{ID: "m2", Role: chat.RoleUser, Attachments: []byte(`[{"kind":"image","artifactId":"reused"},{"kind":"image","artifactId":"gone"}]`)},
	}}

	for _, name := range []string{"logo.png", "screenshot.png"} {
		out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1", ProjectID: &project}, map[string]any{"name": name, "question": "q"})
		if !strings.HasPrefix(out, "Vision answer about "+name) {
			t.Fatalf("%s not found:\n%s", name, out)
		}
	}
	out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, map[string]any{"name": "other.png", "question": "q"})
	if !strings.HasPrefix(out, "No image named") || strings.Contains(out, "gone.png") {
		t.Fatalf("another thread's or a deleted image was reachable:\n%s", out)
	}
}

func TestViewImage_visionFailureIsToolFailure(t *testing.T) {
	client := &fakeToolChatClient{ImageAskErr: errors.New("vision model down")}
	s := viewImageEngine(t, client)

	out := s.viewImageDigest(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, map[string]any{"question": "q"})

	if !strings.HasPrefix(out, "tool failed:") {
		t.Fatalf("want a tool failure, got:\n%s", out)
	}
}

// A turn that calls view_image gets the vision answer back as a plain tool
// message: no image part enters the turn, so it stays on the chat model. The
// prompt tells the model about the tool, and the call logs as its own purpose.
func TestTurnViewsImageAsText(t *testing.T) {
	client := &fakeToolChatClient{
		Results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{{ID: "call_view", Type: "function", Function: llm.ToolCallFunction{
				Name: viewImageToolName, Arguments: `{"name":"hr.png","question":"Is the heart rate line compressed?"}`,
			}}}},
			{Content: "The axis runs to 250, so the line looks flat."},
		},
		ImageAnswer: "The y axis spans 0 to 250; the line sits between 120 and 160.",
	}
	e := viewImageEngine(t, client)
	store := &fakeThreadStore{Thread: chat.Thread{ID: "t1", UserID: testUser.ID, Title: "Swim"}}
	out := runStoredTurn(t, Config{LLM: client, Artifacts: e.artifacts, UsersDir: e.usersDir}, store, "why does the HR look compressed?")
	if out.err != nil {
		t.Fatalf("turn failed: %v: %s", out.err, out.body)
	}
	if !toolNames(client.Tools[0])[viewImageToolName] {
		t.Fatal("view_image not offered")
	}
	if !strings.Contains(client.Histories[0][0].Content, viewImageGuidancePrompt) {
		t.Fatal("view_image guidance missing from the system prompt")
	}
	if len(client.ImageAsks) != 1 {
		t.Fatalf("AskImage calls %+v, want one", client.ImageAsks)
	}
	second := client.Histories[1]
	last := second[len(second)-1]
	if last.Role != "tool" || !strings.Contains(last.Content, "0 to 250") {
		t.Fatalf("last message of round 2 = %+v, want the vision answer as a tool message", last)
	}
	for _, m := range second {
		if len(m.ContentParts) > 0 {
			t.Fatalf("an image part entered the turn: %+v", m)
		}
	}
}

// view_image's answer is image content, outside text: a tool acting for the
// user refuses after it.
func TestViewImageCountsAsOutsideContent(t *testing.T) {
	client := &fakeToolChatClient{ImageAnswer: "ok"}
	e := viewImageEngine(t, client)
	run := &Run{e: e, user: testUser, thread: chat.Thread{ID: "t1"}, offered: map[string]*toolSpec{viewImageToolName: &viewImageToolSpec}}
	_, _, handled := run.executeBuiltInTool(context.Background(), llm.ToolCall{Function: llm.ToolCallFunction{Name: viewImageToolName, Arguments: `{"question":"q"}`}}, nil)
	if !handled || !run.readOutside {
		t.Fatalf("handled=%v readOutside=%v, want both true", handled, run.readOutside)
	}
}

// view_image reads image content, outside text: it must not count as sharing
// the user's intent, and a scheduled run never gets it.
func TestViewImage_spec(t *testing.T) {
	s := &Engine{artifacts: fakeArtifactStore{}, usersDir: "/users"}
	_, byName := s.registry()
	spec := byName[viewImageToolName]
	if spec == nil {
		t.Fatal("view_image is not registered")
	}
	if spec.sharesIntent || spec.userIntent || spec.scheduledLastStep {
		t.Errorf("view_image flags: sharesIntent=%v userIntent=%v scheduledLastStep=%v, want all false",
			spec.sharesIntent, spec.userIntent, spec.scheduledLastStep)
	}
	if !spec.offered(s, chat.Thread{ID: "t1"}, toolGate{}) {
		t.Error("view_image not offered where artifacts can be stored")
	}
	if spec.offered(&Engine{}, chat.Thread{ID: "t1"}, toolGate{}) {
		t.Error("view_image offered without an artifact store")
	}
}
