package turn

import (
	"errors"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/turn/turntest"
)

// docToolTurn is a writing turn whose model calls the document tool name
// with args, then answers.
func docToolTurn(t *testing.T, cfg Config, name, args string) (turnOutcome, *fakeToolChatClient) {
	t.Helper()
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{
		ClassifyResult: string(classifier.WritingEditing),
		Results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: name, Arguments: args}}}},
			{Content: "Done."},
		},
	}
	cfg.LLM = llmClient
	return runStoredTurn(t, cfg, store, "make me a markdown file"), llmClient
}

func lastToolResult(llmClient *fakeToolChatClient) string {
	history := llmClient.Histories[len(llmClient.Histories)-1]
	return history[len(history)-1].Content
}

// A document from the docgen sidecar becomes an artifact of the turn through
// loom's own MCP client, streamed to the browser under loom's type for it;
// the model sees one line naming it, and nothing counts as usage.
func TestTurnKeepsSidecarDocumentAsArtifact(t *testing.T) {
	var created artifact.CreateInput
	counter := &countingUsage{counts: map[string]int{}}
	out, llmClient := docToolTurn(t, Config{
		Usage:     counter,
		MCP:       turntest.DocgenService(t),
		Artifacts: fakeArtifactStore{Created: &created},
		UsersDir:  t.TempDir(),
	}, turntest.DocgenTool("create_text_file"), `{"filename":"notes","extension":"md","content":"# Notes"}`)
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	if n := strings.Count(out.body, "event: artifact"); n != 1 {
		t.Fatalf("%d artifact events, want 1; body=%s", n, out.body)
	}
	if created.MIMEType != "text/markdown; charset=utf-8" || created.SizeBytes != 7 || !strings.HasSuffix(created.DisplayFilename, "notes.md") {
		t.Errorf("created %+v", created)
	}
	if got := lastToolResult(llmClient); got != "created artifact notes.md (7 bytes)" {
		t.Errorf("tool result the model saw: %q", got)
	}
	if len(counter.counts) != 0 {
		t.Errorf("counted %v, want nothing", counter.counts)
	}
	// The document tools were offered long-running.
	for _, tool := range llmClient.Tools[0] {
		if strings.HasPrefix(tool.Function.Name, mcp.DocgenServer+"__") && !tool.LongRunning {
			t.Errorf("%s offered without the document budgets", tool.Function.Name)
		}
	}
}

// A generator's refusal reaches the model as a failed call.
func TestTurnSidecarDocumentErrorIsToolFailure(t *testing.T) {
	out, llmClient := docToolTurn(t, Config{
		MCP:       turntest.DocgenService(t),
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}, turntest.DocgenTool("create_text_file"), `{"filename":"notes"}`)
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	if got := lastToolResult(llmClient); !strings.HasPrefix(got, toolFailedPrefix) || !strings.Contains(got, "content is required") {
		t.Errorf("tool result the model saw: %q", got)
	}
}

// Refused documents are named to the model, one that cannot be stored reads
// as a failure, and one with nowhere to go is said so.
func TestTurnDocumentNotes(t *testing.T) {
	tool := []llm.Tool{turntest.DocgenToolDef("create_pdf_file")}
	doc := []mcp.Document{{Name: "r.pdf", Extension: "pdf", Data: []byte("%PDF-1.7")}}
	name := turntest.DocgenTool("create_pdf_file")

	out, llmClient := docToolTurn(t, Config{
		MCP:       fakeMCPService{ToolList: tool, DroppedDocuments: 2},
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}, name, `{}`)
	if got := lastToolResult(llmClient); out.err != nil || !strings.Contains(got, "2 file(s) the tool returned were refused") {
		t.Errorf("refused: %q, %v", got, out.err)
	}

	out, llmClient = docToolTurn(t, Config{
		MCP:       fakeMCPService{ToolList: tool, Documents: doc},
		Artifacts: fakeArtifactStore{CreateErr: errors.New("disk full")},
		UsersDir:  t.TempDir(),
	}, name, `{}`)
	if got := lastToolResult(llmClient); out.err != nil || !strings.HasPrefix(got, toolFailedPrefix) || !strings.Contains(got, "disk full") {
		t.Errorf("not stored: %q, %v", got, out.err)
	}

	run := &Run{e: &Engine{}}
	note, created := run.keepToolArtifacts(t.Context(), name, toolRun{documents: doc})
	if created != nil || !strings.Contains(note, "1 file(s) could not be kept") {
		t.Errorf("no storage: %q, %v", note, created)
	}
}

// A scheduled run never gets a document tool: it researches unattended.
func TestScheduledRunGetsNoDocumentTool(t *testing.T) {
	run := &Run{e: &Engine{}, scheduled: &ScheduledRun{}}
	tools, _ := run.allowTools([]llm.Tool{
		turntest.DocgenToolDef("create_pdf_file"),
		{Function: llm.ToolFunction{Name: "fetch__fetch"}},
	}, map[string]*toolSpec{})
	if names := orderedToolNames(tools); len(names) != 1 || names[0] != "fetch__fetch" {
		t.Fatalf("scheduled tools = %v, want fetch only", names)
	}
}
