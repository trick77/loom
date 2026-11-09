package turn

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/sandbox"
)

// Of loom's own tools a scheduled run keeps run_python only: no memory,
// schedule or other built-in.
func TestScheduledRunKeepsOnlyPythonOfTheBuiltIns(t *testing.T) {
	specs, _ := (&Engine{}).registry()
	builtIns := map[string]*toolSpec{}
	var tools []llm.Tool
	for i := range specs {
		builtIns[specs[i].name] = &specs[i]
		tools = append(tools, specs[i].tool)
	}
	tools = append(tools, llm.Tool{Function: llm.ToolFunction{Name: "fetch__fetch"}})
	run := &Run{e: &Engine{}, scheduled: &ScheduledRun{}}
	kept, offered := run.allowTools(tools, builtIns)
	if names := orderedToolNames(kept); len(names) != 2 || names[0] != sandboxToolName || names[1] != "fetch__fetch" {
		t.Fatalf("scheduled tools = %v, want run_python and fetch", names)
	}
	if len(offered) != 1 || offered[sandboxToolName] == nil {
		t.Fatalf("scheduled built-ins = %v, want run_python only", offered)
	}
	if run.scheduledDenies(sandboxToolName) {
		t.Fatal("run_python offered but denied")
	}
}

// A scheduled run draws a chart from a tool's files in a research round; the
// tool-free answer step names it. Nothing run_python printed can leave: the
// user's documents stay out of reach, and after it no other tool runs (an
// injected page could have the run print a file and fetch it out).
func TestScheduledRunDrawsAChartFromToolFiles(t *testing.T) {
	secret := rag.Document{ID: "doc-secret", Filename: "salary.csv", VolumeRelpath: "files/salary.csv"}
	secretAlias := sandboxAlias(secret)
	var chart bytes.Buffer
	if err := png.Encode(&chart, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	call := func(id, name, args string) llm.StreamResult {
		return llm.StreamResult{ToolCalls: []llm.ToolCall{{ID: id, Type: "function", Function: llm.ToolCallFunction{Name: name, Arguments: args}}}}
	}
	client := &fakeToolChatClient{
		Results: []llm.StreamResult{
			call("c1", "strava__weekly_load", `{}`),
			call("c2", sandboxToolName, `{"code":"draw()","files":["strava_weeks.csv"]}`),
			call("c3", sandboxToolName, `{"code":"print(open('in/`+secretAlias+`').read())","files":["`+secretAlias+`"]}`),
			call("c4", "strava__weekly_load", `{"note":"week,tss 1,300"}`),
			{Content: "DONE"},
		},
		Plain: "Your eight weeks, chart attached.",
	}
	box := &fakeSandbox{Enabled: true, Result: sandbox.Result{Stdout: "ok\n", Files: []sandbox.File{{Name: "tss.png", Data: chart.Bytes()}}}}
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Weekly"}}
	mcpCalls := 0
	out := runScheduledTurn(t, Config{
		LLM: client,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "strava__weekly_load"}}},
			CallFunc: func(context.Context, string, map[string]any) (string, error) {
				mcpCalls++
				return "Eight weeks of load.", nil
			},
			Files: []mcp.File{{Name: "strava_weeks.csv", MimeType: "text/csv", Data: []byte("week,tss\n1,300\n")}},
		},
		Documents: &listDocuments{Docs: []rag.Document{secret}},
		Sandbox:   box,
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}, store, "weekly TSS summary with a stacked chart")
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	if !toolNames(client.Tools[0])[sandboxToolName] {
		t.Fatal("run_python not offered to the research rounds")
	}
	system := client.Histories[0][0].Content
	if !strings.Contains(system, sandboxGuidancePrompt) || !strings.Contains(system, scheduledSandboxNote) {
		t.Fatal("research prompt lacks the run_python guidance or the chart note")
	}
	if strings.Contains(system, secretAlias) {
		t.Fatal("the guidance lists a user document")
	}
	if len(box.Got) != 1 || len(box.Got[0].Files) != 1 || box.Got[0].Files[0].Name != "strava_weeks.csv" {
		t.Fatalf("sandbox got %+v, want the tool file only", box.Got)
	}
	if got := client.Histories[3][len(client.Histories[3])-1].Content; !strings.Contains(got, "unknown input file") {
		t.Fatalf("a document reached run_python: %q", got)
	}
	if got := client.Histories[4][len(client.Histories[4])-1].Content; got != afterLastStepRefusal || mcpCalls != 1 {
		t.Fatalf("a tool ran after run_python: %d calls, result %q", mcpCalls, got)
	}
	final := client.Histories[len(client.Histories)-1]
	if len(client.Tools[len(client.Tools)-1]) != 0 {
		t.Fatal("the answer step offered tools")
	}
	if strings.Contains(final[0].Content, scheduledSandboxNote) || strings.Contains(final[0].Content, scheduledResearchNote) ||
		strings.Contains(final[0].Content, sandboxGuidancePrompt) {
		t.Fatal("the answer step still reads the research note or the run_python guidance")
	}
	if !strings.Contains(final[0].Content, scheduledChartAnswerNote) {
		t.Fatal("the answer step is not told how to refer to the chart")
	}
	if !strings.Contains(final[len(final)-1].Content, "created artifact tss.png") {
		t.Fatal("the answer step does not know the chart exists")
	}
	if len(out.result.Artifacts) != 1 || out.result.Artifacts[0].DisplayFilename != "tss.png" {
		t.Fatalf("artifacts %+v", out.result.Artifacts)
	}
	if store.AssistantContent != "Your eight weeks, chart attached." {
		t.Fatalf("answer %q", store.AssistantContent)
	}
}

// A job on the model's own numbers reads none of the run's tool files, so
// the run keeps researching after it.
func TestScheduledPythonWithoutToolFilesKeepsToolsOpen(t *testing.T) {
	client := &fakeToolChatClient{
		Results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: sandboxToolName, Arguments: `{"code":"print(300+250)"}`}}}},
			{ToolCalls: []llm.ToolCall{{ID: "c2", Type: "function", Function: llm.ToolCallFunction{Name: "fetch__fetch", Arguments: `{"url":"https://example.com"}`}}}},
			{Content: "DONE"},
		},
		Plain: "550 in total.",
	}
	calls := 0
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Weekly"}}
	out := runScheduledTurn(t, Config{
		LLM: client,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "fetch__fetch"}}},
			CallFunc: func(context.Context, string, map[string]any) (string, error) { calls++; return "page", nil },
		},
		Sandbox:   &fakeSandbox{Enabled: true, Result: sandbox.Result{Stdout: "550\n"}},
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}, store, "sum my two weeks")
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	if calls != 1 {
		t.Fatalf("fetch after a file-less job ran %d times, want 1", calls)
	}
}

// Scheduled runs' jobs take turns on one slot; a run whose context ends
// while it waits gets none.
func TestScheduledSandboxSlotIsShared(t *testing.T) {
	e := New(Config{})
	release, ok := e.scheduledSandboxSlot(t.Context())
	if !ok {
		t.Fatal("free slot not taken")
	}
	waiting, cancel := context.WithCancel(t.Context())
	cancel()
	if _, ok := e.scheduledSandboxSlot(waiting); ok {
		t.Fatal("second job got the held slot")
	}
	release()
	if release, ok = e.scheduledSandboxSlot(t.Context()); !ok {
		t.Fatal("released slot not taken again")
	}
	release()
}

// With the sidecar down a scheduled run's prompt never names run_python.
func TestScheduledRunOmitsPythonWhenSandboxDown(t *testing.T) {
	client := &fakeToolChatClient{Results: []llm.StreamResult{{Content: "DONE"}}, Plain: "Nothing new."}
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Weekly"}}
	out := runScheduledTurn(t, Config{
		LLM:       client,
		MCP:       fakeMCPService{ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "fetch__fetch"}}}},
		Sandbox:   &fakeSandbox{Enabled: false},
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}, store, "anything new?")
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	if toolNames(client.Tools[0])[sandboxToolName] || strings.Contains(client.Histories[0][0].Content, "run_python") {
		t.Fatal("run_python offered or named while the sandbox is down")
	}
}
