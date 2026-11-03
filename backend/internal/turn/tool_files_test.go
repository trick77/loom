package turn

import (
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/sandbox"
)

// A tool's CSV reaches run_python within the turn, named to the model in one
// line; the data itself never enters the prompt or the stored answer.
func TestTurnHandsToolFilesToPython(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	const csv = "elapsed_s,watts\n0,200\n5,250\n"
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "strava__activity_analysis", Arguments: `{}`}}}},
		{ToolCalls: []llm.ToolCall{{ID: "c2", Type: "function", Function: llm.ToolCallFunction{
			Name: sandboxToolName, Arguments: `{"code":"print(1)","files":["strava_1_streams.csv"]}`,
		}}}},
		{ToolCalls: []llm.ToolCall{{ID: "c3", Type: "function", Function: llm.ToolCallFunction{
			Name: sandboxToolName, Arguments: `{"code":"print(1)","files":["nope.csv"]}`,
		}}}},
		{Content: "Chart drawn."},
	}}
	box := &fakeSandbox{Enabled: true, Result: sandbox.Result{Stdout: "1\n"}}
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "strava__activity_analysis"}}},
			Result:   "Analysis text.",
			Files:    []mcp.File{{Name: "strava_1_streams.csv", MimeType: "text/csv", Data: []byte(csv)}},
		},
		Sandbox:   box,
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}, store, "chart my ride")
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	if len(box.Got) != 1 || len(box.Got[0].Files) != 1 || box.Got[0].Files[0].Name != "strava_1_streams.csv" || string(box.Got[0].Files[0].Data) != csv {
		t.Fatalf("sandbox got %+v", box.Got)
	}
	toolMsg := llmClient.Histories[1][len(llmClient.Histories[1])-1].Content
	if !strings.Contains(toolMsg, "in this turn only (in a later turn, call this tool again first): strava_1_streams.csv]") || strings.Contains(toolMsg, "elapsed_s") {
		t.Errorf("tool result the model saw: %q", toolMsg)
	}
	last := llmClient.Histories[3][len(llmClient.Histories[3])-1].Content
	if !strings.Contains(last, "unknown input file") || !strings.Contains(last, "strava_1_streams.csv") {
		t.Errorf("an unknown name must list the turn's files: %q", last)
	}
	if strings.Contains(out.body, "elapsed_s") || strings.Contains(store.AssistantContent, "elapsed_s") {
		t.Error("the CSV leaked into the stream or the answer")
	}
}

func TestKeepToolFilesCaps(t *testing.T) {
	f := newSandboxFixture(t)
	run := f.turn(f.thread)
	big := make([]byte, maxTurnToolFileBytes/2+1)
	if got := run.keepToolFiles([]mcp.File{{Name: "a.csv", Data: big}, {Name: "b.csv", Data: big}}); !strings.Contains(got, "a.csv") || strings.Contains(got, "b.csv") {
		t.Errorf("byte cap: %q", got)
	}
	// Replacing a file of the same name frees its bytes.
	if got := run.keepToolFiles([]mcp.File{{Name: "a.csv", Data: []byte("x")}}); !strings.Contains(got, "a.csv") {
		t.Errorf("replace: %q", got)
	}
	var many []mcp.File
	for i := range maxTurnToolFiles + 2 {
		many = append(many, mcp.File{Name: string(rune('c'+i)) + ".csv", Data: []byte("x")})
	}
	run.keepToolFiles(many)
	if len(run.toolFiles) != maxTurnToolFiles {
		t.Errorf("count cap: %d files", len(run.toolFiles))
	}
	// A turn that does not offer run_python (a scheduled run) keeps nothing.
	noPython := offeredRun(f.srv, f.thread, toolGate{category: "general", sandbox: false})
	if got := noPython.keepToolFiles([]mcp.File{{Name: "z.csv", Data: []byte("x")}}); got != "" || len(noPython.toolFiles) != 0 {
		t.Errorf("a turn without run_python kept files: %q", got)
	}
	if run.keepToolFiles(nil) != "" {
		t.Error("no files, no line")
	}
}
