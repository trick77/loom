package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/sandbox"
	"github.com/trick77/loom/internal/sse"
)

func strp(s string) *string { return &s }

type sandboxFixture struct {
	srv    *Engine
	box    *fakeSandbox
	thread chat.Thread
	body   *httptest.ResponseRecorder
	stream Emitter
}

func newSandboxFixture(t *testing.T) sandboxFixture {
	t.Helper()
	usersDir := t.TempDir()
	write := func(rel, data string) {
		abs := filepath.Join(usersDir, testUser.ID, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("files/umsatz.csv", "kunde,umsatz\nA,10\n")
	write("files/other.csv", "secret")
	write("projects/p1/notes.md", "# notes")
	write("files/global.json", "{}")
	thread := chat.Thread{ID: "t1", ProjectID: strp("p1")}
	docs := &listDocuments{Docs: []rag.Document{
		{ID: "doc-aaaa1111", ThreadID: strp("t1"), Filename: "Umsatz Übersicht (2025).csv", VolumeRelpath: "files/umsatz.csv"},
		{ID: "doc-bbbb2222", ProjectID: strp("p1"), Filename: "notes.md", VolumeRelpath: "projects/p1/notes.md"},
		// Same user, another thread: never offered, never readable.
		{ID: "doc-cccc3333", ThreadID: strp("t2"), Filename: "other.csv", VolumeRelpath: "files/other.csv"},
		// Not a type the sandbox reads.
		{ID: "doc-dddd4444", ThreadID: strp("t1"), Filename: "report.pdf", VolumeRelpath: "files/report.pdf"},
		// Gone from the volume.
		{ID: "doc-eeee5555", ThreadID: strp("t1"), Filename: "stale.csv", VolumeRelpath: "files/stale.csv", Status: rag.StatusStale},
		// User-global: in every thread's scope, as for knowledge.
		{ID: "doc-ffff6666", Filename: "global.json", VolumeRelpath: "files/global.json"},
	}}
	box := &fakeSandbox{Enabled: true}
	rec := httptest.NewRecorder()
	stream, err := sse.NewWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	srv := &Engine{
		sandbox:   box,
		documents: docs,
		usersDir:  usersDir,
		usage:     stubUsageStore{},
		artifacts: fakeArtifactStore{},
	}
	return sandboxFixture{srv: srv, box: box, thread: thread, body: rec, stream: stream}
}

// turn is testUser's turn in thread, emitting to the fixture's stream.
func (f sandboxFixture) turn(thread chat.Thread) *Run {
	return &Run{e: f.srv, stream: f.stream, user: testUser, thread: thread}
}

func runCall(args string) llm.ToolCall {
	return llm.ToolCall{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: sandboxToolName, Arguments: args}}
}

func TestSandboxOfferedFollowsHealthAndConfig(t *testing.T) {
	f := newSandboxFixture(t)
	if !f.srv.sandboxOffered() {
		t.Fatal("healthy sandbox not offered")
	}
	names := toolNames(f.srv.availableTools(f.thread, toolGate{category: "general", sandbox: true}))
	if !names[sandboxToolName] {
		t.Fatalf("run_python missing from %v", names)
	}

	f.box.Enabled = false
	if f.srv.sandboxOffered() || toolNames(f.srv.availableTools(f.thread, toolGate{category: "general", sandbox: false}))[sandboxToolName] {
		t.Fatal("unhealthy sandbox still offered")
	}
	f.box.Enabled = true
	f.srv.artifacts = nil
	if f.srv.sandboxOffered() {
		t.Fatal("offered without an artifact store")
	}
	if (&Engine{}).sandboxOffered() {
		t.Fatal("offered without a sandbox")
	}
}

func toolNames(tools []llm.Tool) map[string]bool {
	out := map[string]bool{}
	for _, tool := range tools {
		out[tool.Function.Name] = true
	}
	return out
}

func TestSandboxGuidanceListsOnlyInScopeInputs(t *testing.T) {
	f := newSandboxFixture(t)
	g := f.srv.sandboxGuidance(context.Background(), testUser.ID, f.thread, nil)
	if !strings.HasPrefix(g, sandboxGuidancePrompt) {
		t.Fatal("guidance must start with the rule")
	}
	notes := strings.Index(g, aliasNotes)
	umsatz := strings.Index(g, aliasUmsatz)
	if notes < 0 || umsatz < 0 || umsatz > notes {
		t.Fatalf("want the store's order (thread file, then project file):\n%s", g)
	}
	// The same list on the next turn: the block must not change between turns.
	if again := f.srv.sandboxGuidance(context.Background(), testUser.ID, f.thread, nil); again != g {
		t.Fatal("guidance changed between identical turns")
	}
	for _, never := range []string{"other.csv", "report.pdf", "stale.csv"} {
		if strings.Contains(g, never) {
			t.Fatalf("%s must not be offered:\n%s", never, g)
		}
	}

	empty := &Engine{sandbox: f.box}
	if got := empty.sandboxGuidance(context.Background(), testUser.ID, f.thread, nil); got != sandboxGuidancePrompt {
		t.Fatalf("no documents: %q", got)
	}
}

func TestSandboxGuidanceOffersUserGlobalDocuments(t *testing.T) {
	f := newSandboxFixture(t)
	g := f.srv.sandboxGuidance(context.Background(), testUser.ID, f.thread, nil)
	if !strings.Contains(g, sandboxAlias(rag.Document{ID: "doc-ffff6666", Filename: "global.json"})) {
		t.Fatalf("user-global document missing:\n%s", g)
	}
}

func TestRunSandboxToolArgumentErrors(t *testing.T) {
	f := newSandboxFixture(t)
	out, _, _ := f.turn(f.thread).executeBuiltInTool(context.Background(),
		runCall(`{"code":"1","files":"`+aliasUmsatz+`"}`))
	if !strings.Contains(out, "files must be an array") {
		t.Fatalf("string files: %q", out)
	}

	var docs []rag.Document
	var names []string
	for i := 0; i < maxSandboxInputFiles+1; i++ {
		d := rag.Document{ID: fmt.Sprintf("d%02d", i), ThreadID: strp("t1"), Filename: "f.csv", VolumeRelpath: "files/umsatz.csv"}
		docs = append(docs, d)
		names = append(names, `"`+sandboxAlias(d)+`"`)
	}
	f.srv.documents = &listDocuments{Docs: docs}
	out, _, _ = f.turn(chat.Thread{ID: "t1"}).executeBuiltInTool(context.Background(),
		runCall(`{"code":"1","files":[`+strings.Join(names, ",")+`]}`))
	if !strings.Contains(out, fmt.Sprintf("at most %d input files", maxSandboxInputFiles)) || len(f.box.Got) != 0 {
		t.Fatalf("too many files: %q", out)
	}
}

func TestRunSandboxToolPassesRejectionReason(t *testing.T) {
	f := newSandboxFixture(t)
	f.box.Err = fmt.Errorf("%w: code exceeds 262144 bytes", sandbox.ErrRejected)
	out, _, _ := f.turn(f.thread).executeBuiltInTool(context.Background(), runCall(`{"code":"1"}`))
	if !strings.Contains(out, "code exceeds 262144 bytes") || strings.Contains(out, "unavailable") {
		t.Fatalf("output %q", out)
	}
}

func TestFormatSandboxResultPutsStdoutLast(t *testing.T) {
	out := formatSandboxResult(sandbox.Result{ExitCode: 1, Stdout: strings.Repeat("x", 40<<10), Stderr: "KeyError"},
		[]ArtifactResponse{{DisplayFilename: "chart.png", SizeBytes: 3}}, []string{"x.svg: file type not allowed"}, time.Minute)
	capped := capToolOutput(out)
	for _, want := range []string{"exit_code: 1", "created artifact chart.png", "x.svg: file type not allowed", "KeyError"} {
		if !strings.Contains(capped, want) {
			t.Fatalf("capping lost %q", want)
		}
	}
	if !strings.HasPrefix(capped[strings.Index(capped, "stdout:\n"):], "stdout:\nxxx") {
		t.Fatal("stdout must come last")
	}
}

// A file attached this turn is named even when the stable list is full and
// the file lies outside the scan (an older project file).
func TestSandboxGuidanceNamesTheTurnsAttachment(t *testing.T) {
	var docs []rag.Document
	for i := 0; i < maxSandboxInputsListed+2; i++ {
		docs = append(docs, rag.Document{ID: fmt.Sprintf("d%02d", i), ThreadID: strp("t1"), Filename: "f.csv"})
	}
	old := rag.Document{ID: "old-project-file", ProjectID: strp("p1"), Filename: "budget.xlsx"}
	s := &Engine{documents: &listDocuments{Docs: docs, Extra: []rag.Document{old}}}
	thread := chat.Thread{ID: "t1", ProjectID: strp("p1")}
	g := s.sandboxGuidance(context.Background(), testUser.ID, thread, []string{"old-project-file", "d00"})
	if !strings.Contains(g, sandboxAlias(old)+` (attached now as "budget.xlsx")`) {
		t.Fatalf("attachment past the cap not named:\n%s", g)
	}
	if strings.Count(g, sandboxAlias(docs[0])) != 1 {
		t.Fatal("an attachment already in the list must not repeat")
	}
	if !strings.Contains(g, "and 2 more") {
		t.Fatalf("remaining count:\n%s", g)
	}
	// Out-of-scope or unreadable attachments stay out.
	other := rag.Document{ID: "x", ThreadID: strp("t9"), Filename: "x.csv"}
	s.documents = &listDocuments{Extra: []rag.Document{other}}
	if g := s.sandboxGuidance(context.Background(), testUser.ID, thread, []string{"x", "missing"}); g != sandboxGuidancePrompt {
		t.Fatalf("out-of-scope attachment named:\n%s", g)
	}
}

func TestSandboxGuidanceCapsTheList(t *testing.T) {
	var docs []rag.Document
	for i := 0; i < maxSandboxInputsListed+3; i++ {
		docs = append(docs, rag.Document{ID: fmt.Sprintf("d%02d", i), ThreadID: strp("t1"), Filename: "f.csv"})
	}
	s := &Engine{documents: &listDocuments{Docs: docs}}
	g := s.sandboxGuidance(context.Background(), testUser.ID, chat.Thread{ID: "t1"}, nil)
	if strings.Count(g, "\n- ") != maxSandboxInputsListed+1 || !strings.Contains(g, "and 3 more") {
		t.Fatalf("list not capped:\n%s", g)
	}
}

var (
	aliasUmsatz = sandboxAlias(rag.Document{ID: "doc-aaaa1111", Filename: "Umsatz Übersicht (2025).csv"})
	aliasNotes  = sandboxAlias(rag.Document{ID: "doc-bbbb2222", Filename: "notes.md"})
)

func TestSandboxAlias(t *testing.T) {
	// "x" stands for the six hex digits of the id hash.
	cases := map[string]string{
		"Übersicht (2025).xlsx":         "Ubersicht_2025_x.xlsx",
		"data.CSV":                      "data_x.csv",
		"???.json":                      "file_x.json",
		"a/../../etc.txt":               "a_etc_x.txt",
		strings.Repeat("y", 80) + ".md": strings.Repeat("y", 40) + "_x.md",
	}
	hash := regexp.MustCompile(`_[0-9a-f]{6}\.`)
	for name, want := range cases {
		got := sandboxAlias(rag.Document{ID: "AbC-123_def", Filename: name})
		if hash.ReplaceAllString(got, "_x.") != want {
			t.Errorf("sandboxAlias(%q) = %q, want the shape %q", name, got, want)
		}
	}
	a := sandboxAlias(rag.Document{ID: "one", Filename: "data.csv"})
	b := sandboxAlias(rag.Document{ID: "two", Filename: "data.csv"})
	if a == b || a != sandboxAlias(rag.Document{ID: "one", Filename: "data.csv"}) {
		t.Fatalf("aliases must be stable per id and differ across ids: %s %s", a, b)
	}
}

func TestRunSandboxToolPassesInputsAndPersistsOutputs(t *testing.T) {
	f := newSandboxFixture(t)
	var pngBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.Black)
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	f.box.Result = sandbox.Result{
		Stdout: "Top: A 10\n",
		Files: []sandbox.File{
			{Name: "chart.png", Data: pngBuf.Bytes()},
			{Name: "top.csv", Data: []byte("kunde,umsatz\nA,10\n")},
			{Name: "fake.png", Data: []byte("<svg onload=alert(1)>")},
			{Name: "x.svg", Data: []byte("<svg/>")},
		},
		Dropped: []string{"link.txt: not a regular file"},
	}
	out, created, handled := f.turn(f.thread).executeBuiltInTool(context.Background(),
		runCall(`{"code":"print(1)","files":["`+aliasUmsatz+`","/work/in/`+aliasNotes+`"]}`))
	if !handled {
		t.Fatal("run_python not handled")
	}
	req := f.box.Got[0]
	if req.Code != "print(1)" || len(req.Files) != 2 || string(req.Files[0].Data) != "kunde,umsatz\nA,10\n" || req.Files[1].Name != aliasNotes {
		t.Fatalf("request %+v", req)
	}
	if len(created) != 2 || created[0].DisplayFilename != "chart.png" || created[1].DisplayFilename != "top.csv" {
		t.Fatalf("created %+v", created)
	}
	for _, want := range []string{"exit_code: 0", "Top: A 10", "created artifact chart.png", "created artifact top.csv",
		"fake.png: not a valid PNG", "x.svg: file type not allowed", "link.txt: not a regular file"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(f.body.Body.String(), "event: artifact") != 2 {
		t.Fatalf("want two artifact events:\n%s", f.body.Body.String())
	}
}

func TestRunSandboxToolRejectsOutOfScopeFiles(t *testing.T) {
	f := newSandboxFixture(t)
	out, _, _ := f.turn(f.thread).executeBuiltInTool(context.Background(),
		runCall(`{"code":"print(1)","files":["`+sandboxAlias(rag.Document{ID: "doc-cccc3333", Filename: "other.csv"})+`"]}`))
	if !strings.HasPrefix(out, "tool failed: unknown input file") || strings.Contains(out[strings.Index(out, "Available"):], "other_") {
		t.Fatalf("output %q", out)
	}
	if !strings.Contains(out, aliasUmsatz) {
		t.Fatalf("the valid names must be listed: %q", out)
	}
	if len(f.box.Got) != 0 {
		t.Fatal("job ran with an unknown file")
	}
}

func TestRunSandboxToolReportsFailures(t *testing.T) {
	cases := []struct {
		name, args string
		err        error
		want       string
	}{
		{"bad json", `{nope`, nil, "tool failed: invalid arguments"},
		{"no code", `{"code":"  "}`, nil, "code is required"},
		{"busy", `{"code":"1"}`, sandbox.ErrBusy, "busy"},
		{"down", `{"code":"1"}`, errors.New("connection refused"), "unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSandboxFixture(t)
			f.box.Err = tc.err
			out, created, _ := f.turn(f.thread).executeBuiltInTool(context.Background(), runCall(tc.args))
			if !strings.HasPrefix(out, toolFailedPrefix) || !strings.Contains(out, tc.want) || created != nil {
				t.Fatalf("output %q created %v", out, created)
			}
		})
	}

	f := newSandboxFixture(t)
	f.box.Enabled = false
	if out, _, _ := f.turn(f.thread).executeBuiltInTool(context.Background(), runCall(`{"code":"1"}`)); !strings.Contains(out, "not available") {
		t.Fatalf("withdrawn sandbox: %q", out)
	}

	f = newSandboxFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.box.Err = context.Canceled
	if out, _, _ := f.turn(f.thread).executeBuiltInTool(ctx, runCall(`{"code":"1"}`)); out != "tool failed: cancelled" {
		t.Fatalf("cancelled: %q", out)
	}
}

func TestRunSandboxToolNonZeroExitIsAResult(t *testing.T) {
	f := newSandboxFixture(t)
	f.box.Result = sandbox.Result{ExitCode: 1, Stderr: "Traceback\nKeyError: 'Umsatz'\n", TimedOut: true}
	out, _, _ := f.turn(f.thread).executeBuiltInTool(context.Background(), runCall(`{"code":"x"}`))
	if strings.HasPrefix(out, toolFailedPrefix) {
		t.Fatalf("a failing program is not a tool failure: %q", out)
	}
	for _, want := range []string{"exit_code: 1", "timed out", "(nothing printed)", "stderr (tail):", "KeyError: 'Umsatz'"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRunSandboxToolInputBudget(t *testing.T) {
	f := newSandboxFixture(t)
	big := filepath.Join(f.srv.usersDir, testUser.ID, "files/umsatz.csv")
	if err := os.WriteFile(big, make([]byte, maxSandboxInputBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, _ := f.turn(f.thread).executeBuiltInTool(context.Background(),
		runCall(`{"code":"1","files":["`+aliasUmsatz+`"]}`))
	if !strings.Contains(out, "too large") || len(f.box.Got) != 0 {
		t.Fatalf("output %q", out)
	}
}

func TestSandboxToolSchema(t *testing.T) {
	tool := sandboxTool()
	raw, err := json.Marshal(tool.Function.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"code"`, `"files"`, `"required":["code"]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("schema lacks %s: %s", want, raw)
		}
	}
	if toolCallCapPerRound(sandboxToolName) != sandboxToolCallsPerRound {
		t.Fatal("per-round cap")
	}
}

func TestTurnRunsPythonAndAnswers(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{{ID: "call_py", Type: "function", Function: llm.ToolCallFunction{
			Name: sandboxToolName, Arguments: `{"code":"print('strawberry'.count('r'))"}`,
		}}}},
		{Content: "There are 3."},
	}}
	box := &fakeSandbox{Enabled: true, Result: sandbox.Result{Stdout: "3\n"}}
	out := runStoredTurn(t, Config{
		LLM:       llmClient,
		Sandbox:   box,
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}, store, "How many r in strawberry?")

	if out.err != nil {
		t.Fatalf("turn failed: %v: %s", out.err, out.body)
	}
	if !strings.Contains(out.body, `"name":"run_python"`) || !strings.Contains(out.body, `exit_code: 0\nstdout:\n3`) {
		t.Fatalf("SSE body lacks the run:\n%s", out.body)
	}
	if store.AssistantContent != "There are 3." {
		t.Fatalf("answer %q", store.AssistantContent)
	}
	if !toolNames(llmClient.Tools[0])[sandboxToolName] {
		t.Fatal("run_python not offered to the model")
	}
	if !strings.Contains(llmClient.Histories[0][0].Content, sandboxGuidancePrompt) {
		t.Fatal("guidance missing from the system prompt")
	}
	if len(box.Got) != 1 || box.Got[0].Code != "print('strawberry'.count('r'))" {
		t.Fatalf("sandbox requests %+v", box.Got)
	}
}

func TestTurnOmitsPythonWhenSandboxDown(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{{Content: "Hi."}}}
	out := runStoredTurn(t, Config{
		LLM:       llmClient,
		Sandbox:   &fakeSandbox{Enabled: false},
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}, store, "Hello")

	if out.err != nil {
		t.Fatalf("turn failed: %v: %s", out.err, out.body)
	}
	if toolNames(llmClient.Tools[0])[sandboxToolName] || strings.Contains(llmClient.Histories[0][0].Content, "run_python") {
		t.Fatal("run_python offered or named while the sandbox is down")
	}
}
