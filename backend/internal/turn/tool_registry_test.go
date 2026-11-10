package turn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/turn/turntest"
	"github.com/trick77/toolkit/llmwire/llmwiretest"
)

// docToolNames are the document tools loom-docgen serves, by exposed name.
var docToolNames = []string{
	turntest.DocgenTool("create_text_file"), turntest.DocgenTool("create_pdf_file"), turntest.DocgenTool("create_xlsx_file"),
	turntest.DocgenTool("create_docx_file"), turntest.DocgenTool("create_pptx_presentation"),
}

// docToolDefs are docToolNames as an MCP tool list carries them.
func docToolDefs() []llm.Tool {
	var out []llm.Tool
	for _, name := range docToolNames {
		out = append(out, llm.Tool{Type: "function", Function: llm.ToolFunction{Name: name, Parameters: map[string]any{"type": "object"}}})
	}
	return out
}

func orderedToolNames(tools []llm.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

// The offered tool set, in order, for representative turns: built-ins first,
// then the MCP tools in their order, the document tools among them only while
// the gate wants files and files can be stored.
func TestAvailableToolsOfferedSetAndOrder(t *testing.T) {
	mcpTools := append([]llm.Tool{
		{Type: "function", Function: llm.ToolFunction{Name: "fetch__fetch"}},
		{Type: "function", Function: llm.ToolFunction{Name: searchToolName}},
		{Type: "function", Function: llm.ToolFunction{Name: "obscura__browser_navigate"}},
	}, docToolDefs()...)
	mcpNames := []string{"fetch__fetch", searchToolName, "obscura__browser_navigate"}
	core := []string{"conversation_search", "read_thread", "remember_user_directive", "forget_user_directive", "update_user_directive", "remember_user_fact", "update_user_fact", "forget_user_fact"}
	project := "p1"
	docgenOn := newToolGate(string(classifier.Coding), "", "")
	docgenOff := newToolGate(string(classifier.General), string(classifier.General), "just chatting")
	withSandbox := func(g toolGate) toolGate { g.sandbox = true; return g }
	concat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	tests := []struct {
		name      string
		artifacts bool
		images    bool
		thread    chat.Thread
		gate      toolGate
		want      []string
	}{
		{
			name: "no project, docgen off, sandbox off, image", artifacts: true, images: true,
			gate: docgenOff,
			want: concat(core, []string{"view_image"}, mcpNames, []string{imageToolName}),
		},
		{
			name: "project, docgen on, sandbox on, image", artifacts: true, images: true,
			thread: chat.Thread{ProjectID: &project}, gate: withSandbox(docgenOn),
			want: concat([]string{"read_project_threads"}, core, []string{"run_python", "view_image"}, mcpNames, docToolNames, []string{imageToolName}),
		},
		{
			name: "no project, docgen on, sandbox off, no image", artifacts: true,
			gate: docgenOn,
			want: concat(core, []string{"view_image"}, mcpNames, docToolNames),
		},
		{
			name: "no project, docgen off, sandbox on, no image", artifacts: true,
			gate: withSandbox(docgenOff),
			want: concat(core, []string{"run_python", "view_image"}, mcpNames),
		},
		{
			name: "no artifact store", images: true,
			gate: withSandbox(docgenOn),
			want: concat(core, mcpNames),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			offered := mcpTools
			if tc.images {
				offered = append(slices.Clone(mcpTools), llm.Tool{Type: "function", Function: llm.ToolFunction{Name: imageToolName}})
			}
			artifacts := turntest.DocgenArtifacts()
			artifacts[mcp.ImagegenServer] = true
			e := &Engine{usersDir: t.TempDir(), mcp: fakeMCPService{ToolList: offered, Artifacts: artifacts}}
			if tc.artifacts {
				e.artifacts = fakeArtifactStore{}
			}
			got := orderedToolNames(e.availableTools(tc.thread, tc.gate))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("offered tools =\n  %v\nwant\n  %v", got, tc.want)
			}
		})
	}
}

// A built-in's definition is built once per engine, not on every turn: every
// turn offers the same shared definition.
func TestBuiltInDefinitionsBuiltOnce(t *testing.T) {
	e := &Engine{}
	params := func() uintptr {
		for _, tool := range e.availableTools(chat.Thread{}, toolGate{}) {
			if tool.Function.Name == conversationSearchToolName {
				return reflect.ValueOf(tool.Function.Parameters).Pointer()
			}
		}
		t.Fatalf("%s not offered", conversationSearchToolName)
		return 0
	}
	first := params()
	for range 2 {
		if params() != first {
			t.Fatal("definition rebuilt for a later turn")
		}
	}
}

// countingUsage records which per-tool counters a call bumped.
type countingUsage struct {
	stubUsageStore
	counts map[string]int
}

func (u *countingUsage) IncWebSearch(context.Context, string) error {
	u.counts["web_search"]++
	return nil
}

func (u *countingUsage) IncWebFetch(context.Context, string) error {
	u.counts["web_fetch"]++
	return nil
}

func (u *countingUsage) IncObscuraFetch(context.Context, string) error {
	u.counts["obscura_fetch"]++
	return nil
}

func (u *countingUsage) IncImageGen(context.Context, string) error {
	u.counts["image_gen"]++
	return nil
}

func (u *countingUsage) IncCodeRun(context.Context, string) error {
	u.counts["code_run"]++
	return nil
}

// The per-round cap, the concurrency flag and the usage counter per tool name.
func TestToolPolicyPerName(t *testing.T) {
	tests := []struct {
		name       string
		cap        int
		concurrent bool
		counter    string
	}{
		{"fetch__fetch", 12, true, "web_fetch"},
		{searchToolName, 8, true, "web_search"},
		{"obscura__browser_navigate", 12, false, "obscura_fetch"},
		{"obscura__browser_snapshot", 12, false, ""},
		{"run_python", 3, false, ""},
		{imageToolName, 1, false, ""},
		{turntest.DocgenTool("create_pdf_file"), 8, false, ""},
		{"conversation_search", 8, false, ""},
		{"context7__query-docs", 8, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := &countingUsage{counts: map[string]int{}}
			e := &Engine{usage: u}
			if got := e.toolCallCapPerRound(tc.name); got != tc.cap {
				t.Errorf("cap = %d, want %d", got, tc.cap)
			}
			if got := e.runsConcurrently(tc.name); got != tc.concurrent {
				t.Errorf("concurrent = %v, want %v", got, tc.concurrent)
			}
			e.countToolCall(context.Background(), testUser, tc.name)
			want := map[string]int{}
			if tc.counter != "" {
				want[tc.counter] = 1
			}
			if !reflect.DeepEqual(u.counts, want) {
				t.Errorf("counters = %v, want %v", u.counts, want)
			}
		})
	}
}

// Every tool of the docgen server is a document tool, whatever its name: a
// replacement server's extra tools are gated like the sidecar's.
func TestDocgenServerPolicyCoversEveryTool(t *testing.T) {
	e := &Engine{}
	for _, name := range append(slices.Clone(docToolNames), turntest.DocgenTool("create_odt_file")) {
		if !e.toolPolicy(name).documentGenerator {
			t.Errorf("%s: not a document tool", name)
		}
	}
	if e.toolPolicy("other__create_pdf_file").documentGenerator || e.toolPolicy("create_pdf_file").documentGenerator {
		t.Error("a tool of another server is a document tool")
	}
}

// The round policy comes from the engine's registry, so one set on a
// built-in's spec takes effect for that tool.
func TestToolPolicyFollowsRegistrySpecs(t *testing.T) {
	u := &countingUsage{counts: map[string]int{}}
	e := &Engine{usage: u}
	_, byName := e.registry()
	spec := byName[conversationSearchToolName]
	spec.capPerRound = 2
	spec.concurrent = true
	spec.counts = &usageCounter{"image_gen", UsageStore.IncImageGen}

	if got := e.toolCallCapPerRound(conversationSearchToolName); got != 2 {
		t.Errorf("cap = %d, want 2", got)
	}
	if !e.runsConcurrently(conversationSearchToolName) {
		t.Error("not concurrent")
	}
	e.countToolCall(context.Background(), testUser, conversationSearchToolName)
	if want := map[string]int{"image_gen": 1}; !reflect.DeepEqual(u.counts, want) {
		t.Errorf("counters = %v, want %v", u.counts, want)
	}
}

// Every document tool as offered widens the LLM client's tool-call idle
// window: a buffering endpoint goes silent mid-argument for longer than the
// normal window. A non-document tool keeps the normal window and stalls.
func TestDocgenToolsWidenToolCallIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"x","arguments":"{\"a\""}}]}}]}` + "\n\n"))
		flusher.Flush()
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":1}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	client, err := llm.NewClient(llm.Config{
		BaseURL:     server.URL,
		Registry:    llmwiretest.Registry(),
		Models:      llm.Roles{Chat: llmwiretest.ChatModel},
		Timeout:     5 * time.Second,
		IdleTimeout: 100 * time.Millisecond,
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	stream := func(tool llm.Tool) error {
		_, err := client.StreamChatWithTools(context.Background(), []llm.Message{{Role: "user", Content: "go"}}, []llm.Tool{tool}, nil)
		return err
	}

	e := &Engine{artifacts: fakeArtifactStore{}, usersDir: t.TempDir(), mcp: fakeMCPService{ToolList: docToolDefs(), Artifacts: turntest.DocgenArtifacts()}}
	offered := map[string]llm.Tool{}
	for _, tool := range e.availableTools(chat.Thread{}, newToolGate(string(classifier.Coding), "", "")) {
		offered[tool.Function.Name] = tool
	}
	// Each case waits out the server's pause, so they run in parallel: the
	// handler keeps no state, and the client and offered are only read. The
	// parent's server cleanup runs after every subtest.
	for _, name := range docToolNames {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tool, ok := offered[name]
			if !ok {
				t.Fatalf("%s not offered", name)
			}
			if err := stream(tool); err != nil {
				t.Errorf("%s: stream error = %v, want the widened idle window", name, err)
			}
		})
	}
	t.Run("non-document tool", func(t *testing.T) {
		t.Parallel()
		if err := stream(offered[conversationSearchToolName]); !errors.Is(err, llm.ErrStreamStalled) {
			t.Fatalf("non-document tool: error = %v, want ErrStreamStalled", err)
		}
	})
}
