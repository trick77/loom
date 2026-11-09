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
	// The header names the columns (a model guessing them asked for "time"
	// in a file whose column is elapsed_s); no data row goes along.
	toolMsg := llmClient.Histories[1][len(llmClient.Histories[1])-1].Content
	if !strings.Contains(toolMsg, "pass the name in run_python's files; in a later turn, call this tool again first): strava_1_streams.csv (columns: elapsed_s,watts)]") ||
		strings.Contains(toolMsg, "0,200") {
		t.Errorf("tool result the model saw: %q", toolMsg)
	}
	last := llmClient.Histories[3][len(llmClient.Histories[3])-1].Content
	if !strings.Contains(last, "unknown input file") || !strings.Contains(last, "strava_1_streams.csv") {
		t.Errorf("an unknown name must list the turn's files: %q", last)
	}
	if strings.Contains(out.body, "0,200") || strings.Contains(store.AssistantContent, "0,200") {
		t.Error("the CSV leaked into the stream or the answer")
	}
}

// A tool file the code opens by name reaches the job even when the model
// left it out of files: it once read in/strava_…_streams.csv twice from an
// empty in/ before it found the argument.
func TestTurnMountsToolFilesTheCodeNames(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "strava__activity_analysis", Arguments: `{}`}}}},
		{ToolCalls: []llm.ToolCall{{ID: "c2", Type: "function", Function: llm.ToolCallFunction{
			Name: sandboxToolName, Arguments: `{"code":"import pandas as pd\npd.read_csv('in/strava_1_streams.csv')"}`,
		}}}},
		{ToolCalls: []llm.ToolCall{{ID: "c3", Type: "function", Function: llm.ToolCallFunction{
			Name: sandboxToolName, Arguments: `{"code":"open('in/strava_1_streams.csv')","files":["strava_1_streams.csv"]}`,
		}}}},
		{ToolCalls: []llm.ToolCall{{ID: "c4", Type: "function", Function: llm.ToolCallFunction{
			Name: sandboxToolName, Arguments: `{"code":"print(1)"}`,
		}}}},
		{Content: "Done."},
	}}
	box := &fakeSandbox{Enabled: true, Result: sandbox.Result{Stdout: "ok\n"}}
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "strava__activity_analysis"}}},
			Result:   "Analysis text.",
			Files: []mcp.File{
				{Name: "strava_1_streams.csv", MimeType: "text/csv", Data: []byte("elapsed_s\n0\n")},
				{Name: "strava_1_climbs.csv", MimeType: "text/csv", Data: []byte("km\n1\n")},
			},
		},
		Sandbox:   box,
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}, store, "chart my ride")
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	if len(box.Got) != 3 {
		t.Fatalf("%d sandbox runs", len(box.Got))
	}
	names := func(i int) []string {
		var n []string
		for _, f := range box.Got[i].Files {
			n = append(n, f.Name)
		}
		return n
	}
	// Named in the code only; named in both, once; named nowhere, none.
	if got := names(0); len(got) != 1 || got[0] != "strava_1_streams.csv" {
		t.Errorf("code-named file: %v", got)
	}
	if got := names(1); len(got) != 1 || got[0] != "strava_1_streams.csv" {
		t.Errorf("named in files and code: %v", got)
	}
	if got := names(2); len(got) != 0 {
		t.Errorf("unnamed files mounted: %v", got)
	}
}

func TestCodeNamedToolFiles(t *testing.T) {
	tf := map[string]mcp.File{"a.csv": {}, "data.csv": {}, "x.csv": {}, "y.csv": {}}
	got := codeNamedToolFiles(`pd.read_csv("in/data.csv"); open('/work/in/y.csv'); f"in/x.csv.bak"; "main/x.csv"`, tf)
	if strings.Join(got, ",") != "data.csv,y.csv" {
		t.Errorf("got %v: a.csv is inside data.csv, x.csv only inside longer names", got)
	}
	if codeNamedToolFiles("print(1)", tf) != nil {
		t.Error("no path in the code, none added")
	}
	// A bare name as a whole string literal counts (a loomcharts call takes
	// the name a tool printed), ./ too; unquoted or inside a longer string it
	// does not.
	got = codeNamedToolFiles(`lc.ride_profile("data.csv", "out/p.png", climbs='./y.csv')  # not a.csv; "x.csv.bak"`, tf)
	if strings.Join(got, ",") != "data.csv,y.csv" {
		t.Errorf("bare quoted names: got %v", got)
	}
	// Paths before quoted names: the caps drop a quoted one (maybe an output
	// name) first.
	got = codeNamedToolFiles(`df.to_csv("a.csv"); pd.read_csv("in/y.csv")`, tf)
	if strings.Join(got, ",") != "y.csv,a.csv" {
		t.Errorf("order: got %v, want the in/ path first", got)
	}
}

// A file the code names but the model did not ask for never fails the call:
// past a cap it is left out.
func TestSandboxFilesCodeNamedWithinCaps(t *testing.T) {
	f := newSandboxFixture(t)
	big := mcp.File{Name: "big.csv", Data: make([]byte, maxSandboxInputBytes)}
	tf := map[string]mcp.File{"big.csv": big, "s.csv": {Name: "s.csv", Data: []byte("a\n")}}
	files, msg := f.srv.sandboxFiles(t.Context(), testUser.ID, f.thread, []any{"s.csv"}, "open('in/big.csv'); open('in/s.csv')", tf, true)
	if msg != "" || len(files) != 1 || files[0].Name != "s.csv" {
		t.Errorf("over the byte cap: %q %v", msg, files)
	}
	if _, msg := f.srv.sandboxFiles(t.Context(), testUser.ID, f.thread, "s.csv", "", tf, true); !strings.Contains(msg, "files must be an array") {
		t.Errorf("malformed files: %q", msg)
	}
}

func TestMissingInputHint(t *testing.T) {
	tf := map[string]mcp.File{"strava_1_streams.csv": {}}
	res := sandbox.Result{ExitCode: 1, Stderr: "FileNotFoundError: [Errno 2] No such file or directory: 'in/strava_1.csv'"}
	if got := missingInputHint(res, nil, tf); !strings.Contains(got, "in/ holds only the files named in files") || !strings.Contains(got, "strava_1_streams.csv") {
		t.Errorf("hint: %q", got)
	}
	res.ExitCode = 0
	if missingInputHint(res, nil, tf) != "" {
		t.Error("a run that worked gets no hint")
	}
	// A mounted file opened bare: the error has no in/, the hint says where
	// it is (pd.read_csv('<name>') still misses it after the mount).
	bare := sandbox.Result{ExitCode: 1, Stderr: "FileNotFoundError: [Errno 2] No such file or directory: 'strava_1_streams.csv'"}
	mounted := []sandbox.File{{Name: "strava_1_streams.csv"}}
	if got := missingInputHint(bare, mounted, tf); got != "note: input files are in in/, not the working directory: read in/strava_1_streams.csv\n" {
		t.Errorf("bare-name hint: %q", got)
	}
	if got := missingInputHint(sandbox.Result{ExitCode: 1, Stderr: "No such file or directory: 'other.csv'"}, mounted, tf); got != "" {
		t.Errorf("an unrelated missing file got a hint: %q", got)
	}
}

func TestCSVColumns(t *testing.T) {
	for _, c := range []struct {
		f    mcp.File
		want string
	}{
		{mcp.File{Name: "s", MimeType: "text/csv; charset=utf-8", Data: []byte("elapsed_s,hr\n0,120\n")}, " (columns: elapsed_s,hr)"},
		{mcp.File{Name: "s.csv", Data: []byte("2026-11-01,42.1,198\n")}, ""}, // no header: data stays out
		{mcp.File{Name: "s.csv", Data: []byte("a],[b\n")}, " (columns: a),(b)"},
		{mcp.File{Name: "s.csv", Data: []byte("H\xf6he,km\n")}, " (columns: Hhe,km)"},
		{mcp.File{Name: "s.png", MimeType: "image/png", Data: []byte("x,y\n")}, ""},
	} {
		if got := csvColumns(c.f); got != c.want {
			t.Errorf("%q: got %q, want %q", c.f.Data, got, c.want)
		}
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
