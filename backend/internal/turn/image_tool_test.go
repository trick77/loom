package turn

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/turn/turntest"
)

// countingImageProvider records every request the image service sent.
type countingImageProvider struct {
	mu       sync.Mutex
	requests []imagegen.GenerateRequest
}

func (p *countingImageProvider) Generate(ctx context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	return turntest.ImageProvider{}.Generate(ctx, req)
}

func imageCall(id, args string) llm.ToolCall {
	return llm.ToolCall{ID: id, Type: "function", Function: llm.ToolCallFunction{Name: imageToolName, Arguments: args}}
}

// The image tool as offered never shows the arguments loom fills, and its
// shared definition is left as the server sent it.
func TestOfferedImageToolHidesLoomArgs(t *testing.T) {
	svc := turntest.ImageService(t, &countingImageProvider{}, "", "")
	e := &Engine{mcp: svc, artifacts: fakeArtifactStore{}, usersDir: t.TempDir()}
	var offered *llm.Tool
	for _, tool := range e.offeredMCPTools(testUser, toolGate{}) {
		if tool.Function.Name == imageToolName {
			offered = &tool
		}
	}
	if offered == nil {
		t.Fatal("image tool not offered")
	}
	props := offered.Function.Parameters["properties"].(map[string]any)
	for _, name := range imageLoomArgs {
		if _, ok := props[name]; ok {
			t.Errorf("offered schema shows %s", name)
		}
	}
	if _, ok := props["prompt"]; !ok {
		t.Error("offered schema lost prompt")
	}
	for _, tool := range svc.Tools() {
		if tool.Function.Name == imageToolName {
			if _, ok := tool.Function.Parameters["properties"].(map[string]any)[argModel]; !ok {
				t.Error("the shared definition lost the loom args")
			}
		}
	}
	// No artifact store: the image tool is not offered at all.
	if tools := (&Engine{mcp: svc}).offeredMCPTools(testUser, toolGate{}); len(tools) != 0 {
		t.Errorf("offered without an artifact store: %v", orderedToolNames(tools))
	}
	// A server not allowed to make artifacts would have every image dropped:
	// the image tool is not offered (as no turn is routed to it).
	plain := &Engine{mcp: fakeMCPService{ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: imageToolName}}}}, artifacts: fakeArtifactStore{}, usersDir: t.TempDir()}
	if tools := plain.offeredMCPTools(testUser, toolGate{}); len(tools) != 0 {
		t.Errorf("offered from a server without artifacts: %v", orderedToolNames(tools))
	}
}

func TestWithoutLoomArgsTrimsRequired(t *testing.T) {
	for _, required := range []any{[]any{"prompt", "model"}, []string{"prompt", "input_images"}} {
		tool := withoutLoomArgs(llm.Tool{Function: llm.ToolFunction{Parameters: map[string]any{
			"properties": map[string]any{"prompt": map[string]any{}, "model": map[string]any{}, "input_images": map[string]any{}},
			"required":   required,
		}}})
		switch got := tool.Function.Parameters["required"].(type) {
		case []any:
			if len(got) != 1 || got[0] != "prompt" {
				t.Errorf("required = %v", got)
			}
		case []string:
			if len(got) != 1 || got[0] != "prompt" {
				t.Errorf("required = %v", got)
			}
		}
	}
}

// Loom-filled arguments reach the server only: the run, the logs and the
// stored call keep the model's own.
func TestRunToolCallWithAddsArgsForTheServerOnly(t *testing.T) {
	var sent map[string]any
	e := &Engine{mcp: fakeMCPService{CallFunc: func(_ context.Context, _ string, args map[string]any) (string, error) {
		sent = args
		return "", nil
	}}}
	call := imageCall("c1", `{"prompt":"a fox"}`)
	run := e.runToolCallWith(context.Background(), call, map[string]any{argModel: "m1"})
	if sent[argModel] != "m1" || sent["prompt"] != "a fox" {
		t.Fatalf("server got %v", sent)
	}
	if _, ok := run.arguments[argModel]; ok || strings.Contains(call.Function.Arguments, argModel) {
		t.Fatalf("loom arg leaked into the run (%v) or the call (%s)", run.arguments, call.Function.Arguments)
	}
}

// The model cannot set what only loom sets: its own model or input image
// never reaches the server, even with no loom value to replace it.
func TestModelCannotSetLoomArgs(t *testing.T) {
	var sent map[string]any
	e := &Engine{mcp: fakeMCPService{CallFunc: func(_ context.Context, _ string, args map[string]any) (string, error) {
		sent = args
		return "", nil
	}}}
	e.runToolCallWith(context.Background(), imageCall("c1", `{"prompt":"x","model":"fal-ai/expensive","Model":"also","input_images":[{"data":"aGk="}]}`), map[string]any{argTextHeavy: false})
	if _, ok := sent[argModel]; ok {
		t.Errorf("model's own model reached the server: %v", sent)
	}
	if _, ok := sent["Model"]; ok {
		t.Errorf("a case variant slipped through (JSON decoders match it): %v", sent)
	}
	if _, ok := sent[argInputImages]; ok {
		t.Errorf("model's own input image reached the server: %v", sent)
	}
	// Another tool's argument of the same name is its own business.
	e.runToolCallWith(context.Background(), llm.ToolCall{Function: llm.ToolCallFunction{Name: "other__tool", Arguments: `{"model":"m"}`}}, nil)
	if sent[argModel] != "m" {
		t.Errorf("a non-image tool lost its model argument: %v", sent)
	}
}

// A scheduled run never gets the image tool: it researches unattended.
func TestScheduledRunGetsNoImageTool(t *testing.T) {
	run := &Run{e: &Engine{mcp: fakeMCPService{}}, scheduled: &ScheduledRun{}}
	tools, _ := run.allowTools([]llm.Tool{
		{Function: llm.ToolFunction{Name: imageToolName}},
		{Function: llm.ToolFunction{Name: "fetch__fetch"}},
	}, map[string]*toolSpec{})
	if names := orderedToolNames(tools); len(names) != 1 || names[0] != "fetch__fetch" {
		t.Fatalf("scheduled tools = %v, want fetch only", names)
	}
}

// An image turn routes only to an image tool this user may use and whose
// images can be kept.
func TestImageToolAvailable(t *testing.T) {
	image := []llm.Tool{{Function: llm.ToolFunction{Name: imageToolName}}}
	cases := []struct {
		name string
		svc  fakeMCPService
		want bool
	}{
		{"artifact server", fakeMCPService{ToolList: image, Available: map[string]bool{imageToolName: true}, Artifacts: map[string]bool{"imagegen": true}}, true},
		{"server without artifacts", fakeMCPService{ToolList: image, Available: map[string]bool{imageToolName: true}}, false},
		{"other users only", fakeMCPService{ToolList: image, Available: map[string]bool{imageToolName: true}, Artifacts: map[string]bool{"imagegen": true}, Users: map[string][]string{"imagegen": {"someone_else"}}}, false},
		{"not configured", fakeMCPService{}, false},
	}
	for _, tc := range cases {
		e := &Engine{mcp: tc.svc, artifacts: fakeArtifactStore{}, usersDir: t.TempDir()}
		if got := e.imageToolAvailable(testUser.ID); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if (&Engine{mcp: cases[0].svc}).imageToolAvailable(testUser.ID) {
		t.Error("available without an artifact store")
	}
}

// The first image locks the thread's model as the server reported it; the
// next image is sent with it, even when its prompt reads as typography work.
func TestImageModelLockedAndEchoed(t *testing.T) {
	provider := &countingImageProvider{}
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Pics"}}
	cfg := Config{
		MCP:       turntest.ImageService(t, provider, "pro", "max"),
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
	}
	cfg.LLM = &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{imageCall("c1", `{"prompt":"a fox"}`)}},
		{Content: "Done."},
	}}
	if out := runStoredTurn(t, cfg, store, "draw a fox"); out.err != nil {
		t.Fatal(out.err)
	}
	if store.Thread.ImageModel != "pro" {
		t.Fatalf("locked model = %q, want pro", store.Thread.ImageModel)
	}
	cfg.LLM = &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{imageCall("c2", `{"prompt":"a logo that says 'FOX'"}`)}},
		{Content: "Done."},
	}}
	if out := runStoredTurn(t, cfg, store, "now a logo"); out.err != nil {
		t.Fatal(out.err)
	}
	if len(provider.requests) != 2 || provider.requests[1].Model != "pro" {
		t.Fatalf("requests = %+v, want the second on the locked model", provider.requests)
	}
}

// A text-heavy first image goes to the typography model: loom's lexical
// scan sets text_heavy when the model called the tool on its own.
func TestImageToolTextHeavyHint(t *testing.T) {
	provider := &countingImageProvider{}
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Pics"}}
	out := runStoredTurn(t, Config{
		MCP:       turntest.ImageService(t, provider, "pro", "max"),
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
		LLM: &fakeToolChatClient{Results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{imageCall("c1", `{"prompt":"a poster that says 'OPEN'"}`)}},
			{Content: "Done."},
		}},
	}, store, "make a poster")
	if out.err != nil {
		t.Fatal(out.err)
	}
	if len(provider.requests) != 1 || provider.requests[0].Model != "max" {
		t.Fatalf("requests = %+v, want the typography model", provider.requests)
	}
}

// A turn makes one image: a second image call is skipped, not run.
func TestImageToolOnePerTurn(t *testing.T) {
	provider := &countingImageProvider{}
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Pics"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{imageCall("c1", `{"prompt":"a fox"}`), imageCall("c2", `{"prompt":"a cat"}`)}},
		{ToolCalls: []llm.ToolCall{imageCall("c3", `{"prompt":"a dog"}`)}},
		{Content: "Done."},
	}}
	out := runStoredTurn(t, Config{
		MCP:       turntest.ImageService(t, provider, "", ""),
		Artifacts: fakeArtifactStore{},
		UsersDir:  t.TempDir(),
		LLM:       llmClient,
	}, store, "draw animals")
	if out.err != nil {
		t.Fatal(out.err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("%d images generated, want 1", len(provider.requests))
	}
	if n := strings.Count(out.body, "event: artifact"); n != 1 {
		t.Fatalf("%d artifact events, want 1", n)
	}
}
