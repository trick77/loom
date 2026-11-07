package turn

import (
	"errors"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

func pngImage(size int) []byte {
	data := make([]byte, size)
	copy(data, "\x89PNG\r\n\x1a\n")
	return data
}

func imageToolTurn(t *testing.T, cfg Config) (turnOutcome, *fakeToolChatClient) {
	t.Helper()
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "pics__draw", Arguments: `{"prompt":"a cat"}`}}}},
		{Content: "Here it is."},
	}}
	cfg.LLM = llmClient
	return runStoredTurn(t, cfg, store, "draw a cat"), llmClient
}

// An image an MCP tool returns becomes an artifact of the turn, streamed to
// the browser and counted as an image; the model sees one line naming it,
// never the bytes.
func TestTurnKeepsMCPToolImagesAsArtifacts(t *testing.T) {
	var created artifact.CreateInput
	counter := &countingUsage{counts: map[string]int{}}
	out, llmClient := imageToolTurn(t, Config{
		Usage: counter,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "pics__draw"}}},
			Result:   "Drawn.",
			Images: []mcp.Image{{
				Filename: "cat", MimeType: "image/png", Extension: "png", Data: pngImage(64),
				Model: "m1", Provider: "p1", Width: 640, Height: 480, DurationMs: 1200,
			}},
		},
		Artifacts: fakeArtifactStore{Created: &created},
		UsersDir:  t.TempDir(),
	})
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	if n := strings.Count(out.body, "event: artifact"); n != 1 {
		t.Fatalf("%d artifact events, want 1", n)
	}
	for _, want := range []string{`"model":"m1"`, `"provider":"p1"`, `"width":640`, `"height":480`, `"durationMs":1200`} {
		if !strings.Contains(out.body, want) {
			t.Errorf("artifact event lacks %s", want)
		}
	}
	if created.MIMEType != "image/png" || created.SizeBytes != 64 || !strings.HasSuffix(created.DisplayFilename, "cat.png") {
		t.Errorf("created %+v", created)
	}
	toolMsg := llmClient.Histories[1][len(llmClient.Histories[1])-1].Content
	if !strings.HasPrefix(toolMsg, "Drawn.") || !strings.Contains(toolMsg, "created image artifact cat.png (64 bytes)") || strings.Contains(toolMsg, "PNG") {
		t.Errorf("tool result the model saw: %q", toolMsg)
	}
	if counter.counts["image_gen"] != 1 {
		t.Errorf("image_gen counted %d times, want 1", counter.counts["image_gen"])
	}
}

// A server that returns artifacts gets the long call deadline.
func TestToolCallDurationForArtifactServers(t *testing.T) {
	e := &Engine{mcp: fakeMCPService{Artifacts: map[string]bool{"pics": true}}}
	if got := e.toolCallDuration("pics__draw"); got != mcp.ArtifactCallTimeout {
		t.Errorf("artifact tool: %v", got)
	}
	if got := e.toolCallDuration("web__search"); got != maxToolCallDuration {
		t.Errorf("text tool: %v", got)
	}
}

// The note survives a result at the size cap; the text gives way.
func TestImageNoteSurvivesFullTextResult(t *testing.T) {
	out, llmClient := imageToolTurn(t, Config{
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "pics__draw"}}},
			Result:   strings.Repeat("x", maxToolResultContentBytes),
			Images:   []mcp.Image{{Filename: "cat", MimeType: "image/png", Extension: "png", Data: pngImage(64)}},
		},
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	})
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	toolMsg := llmClient.Histories[1][len(llmClient.Histories[1])-1].Content
	if len(toolMsg) > maxToolResultContentBytes || !strings.HasSuffix(toolMsg, "created image artifact cat.png (64 bytes)") {
		t.Errorf("tool result %d bytes, ends %q", len(toolMsg), toolMsg[max(0, len(toolMsg)-60):])
	}
}

// Images mcp refused are named to the model, so it does not claim them.
func TestTurnNotesRefusedToolImages(t *testing.T) {
	out, llmClient := imageToolTurn(t, Config{
		MCP: fakeMCPService{
			ToolList:      []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "pics__draw"}}},
			Result:        "Here is your picture.",
			DroppedImages: 1,
		},
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	})
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	toolMsg := llmClient.Histories[1][len(llmClient.Histories[1])-1].Content
	if !strings.Contains(toolMsg, "1 image(s) the tool returned were refused") {
		t.Errorf("tool result the model saw: %q", toolMsg)
	}
}

// An image-only result that cannot be stored reads as a failed call.
func TestTurnImageOnlyResultFailsWhenNotStored(t *testing.T) {
	out, llmClient := imageToolTurn(t, Config{
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "pics__draw"}}},
			Images:   []mcp.Image{{MimeType: "image/png", Extension: "png", Data: pngImage(64)}},
		},
		Artifacts: fakeArtifactStore{CreateErr: errors.New("disk full")},
		UsersDir:  t.TempDir(),
	})
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	toolMsg := llmClient.Histories[1][len(llmClient.Histories[1])-1].Content
	if !strings.HasPrefix(toolMsg, toolFailedPrefix) || !strings.Contains(toolMsg, "disk full") {
		t.Errorf("tool result the model saw: %q", toolMsg)
	}
}

// Without anywhere to store files the image is dropped and the model is told.
func TestTurnDropsMCPToolImagesWithoutArtifactStore(t *testing.T) {
	out, llmClient := imageToolTurn(t, Config{
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "pics__draw"}}},
			Result:   "Drawn.",
			Images:   []mcp.Image{{MimeType: "image/png", Extension: "png", Data: pngImage(64)}},
		},
	})
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	if strings.Contains(out.body, "event: artifact") {
		t.Fatal("artifact streamed without a store")
	}
	toolMsg := llmClient.Histories[1][len(llmClient.Histories[1])-1].Content
	if !strings.Contains(toolMsg, "could not be kept") {
		t.Errorf("tool result the model saw: %q", toolMsg)
	}
}
