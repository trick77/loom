package turn

import (
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
// the browser; the model sees one line naming it, never the bytes.
func TestTurnKeepsMCPToolImagesAsArtifacts(t *testing.T) {
	var created artifact.CreateInput
	out, llmClient := imageToolTurn(t, Config{
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "pics__draw"}}},
			Result:   "Drawn.",
			Images: []mcp.Image{{
				Filename: "cat", MimeType: "image/png", Data: pngImage(64),
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
}

// Without anywhere to store files the image is dropped and the model is told.
func TestTurnDropsMCPToolImagesWithoutArtifactStore(t *testing.T) {
	out, llmClient := imageToolTurn(t, Config{
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "pics__draw"}}},
			Result:   "Drawn.",
			Images:   []mcp.Image{{MimeType: "image/png", Data: pngImage(64)}},
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
