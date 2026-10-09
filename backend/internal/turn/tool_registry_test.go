package turn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/trick77/llmwire/llmwiretest"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
)

// allDocTools is the docgen set cmd/loom wires up.
func allDocTools() []docgen.Generator {
	return []docgen.Generator{
		docgen.TextGenerator{},
		docgen.NewPDFGenerator(nil),
		docgen.XLSXGenerator{},
		docgen.DOCXGenerator{},
		docgen.PPTXGenerator{},
	}
}

func orderedToolNames(tools []llm.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

// The offered tool set, in order, for representative turns. MCP's
// create_text_file collides with the built-in: it is dropped while the
// built-in is offered and passes through while docgen is gated off.
func TestAvailableToolsOfferedSetAndOrder(t *testing.T) {
	mcpTools := []llm.Tool{
		{Type: "function", Function: llm.ToolFunction{Name: "fetch__fetch"}},
		{Type: "function", Function: llm.ToolFunction{Name: tavilySearchExposedName}},
		{Type: "function", Function: llm.ToolFunction{Name: "obscura__browser_navigate"}},
		{Type: "function", Function: llm.ToolFunction{Name: "create_text_file"}},
	}
	mcpNames := []string{"fetch__fetch", tavilySearchExposedName, "obscura__browser_navigate"}
	core := []string{"conversation_search", "read_thread", "remember_user_directive", "forget_user_directive", "update_user_directive"}
	docs := []string{"create_text_file", "create_pdf_file", "create_xlsx_file", "create_docx_file", "create_pptx_presentation"}
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
			want: concat(core, []string{"generate_image"}, mcpNames, []string{"create_text_file"}),
		},
		{
			name: "project, docgen on, sandbox on, image", artifacts: true, images: true,
			thread: chat.Thread{ProjectID: &project}, gate: withSandbox(docgenOn),
			want: concat([]string{"read_project_threads"}, core, docs, []string{"run_python", "generate_image"}, mcpNames),
		},
		{
			name: "no project, docgen on, sandbox off, no image", artifacts: true,
			gate: docgenOn,
			want: concat(core, docs, mcpNames),
		},
		{
			name: "no project, docgen off, sandbox on, no image", artifacts: true,
			gate: withSandbox(docgenOff),
			want: concat(core, []string{"run_python"}, mcpNames, []string{"create_text_file"}),
		},
		{
			name: "no artifact store", images: true,
			gate: withSandbox(docgenOn),
			want: concat(core, mcpNames, []string{"create_text_file"}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &Engine{
				usersDir: t.TempDir(),
				docTools: allDocTools(),
				mcp:      fakeMCPService{ToolList: mcpTools},
			}
			if tc.artifacts {
				e.artifacts = fakeArtifactStore{}
			}
			if tc.images {
				e.imageTools = []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})}
			}
			got := orderedToolNames(e.availableTools(tc.thread, tc.gate))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("offered tools =\n  %v\nwant\n  %v", got, tc.want)
			}
		})
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
		{tavilySearchExposedName, 8, true, "web_search"},
		{"obscura__browser_navigate", 12, false, "obscura_fetch"},
		{"obscura__browser_snapshot", 12, false, ""},
		{"run_python", 3, false, ""},
		{"generate_image", 8, false, ""},
		{"create_pdf_file", 8, false, ""},
		{"conversation_search", 8, false, ""},
		{"context7__query-docs", 8, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolCallCapPerRound(tc.name); got != tc.cap {
				t.Errorf("cap = %d, want %d", got, tc.cap)
			}
			if got := runsConcurrently(tc.name); got != tc.concurrent {
				t.Errorf("concurrent = %v, want %v", got, tc.concurrent)
			}
			u := &countingUsage{counts: map[string]int{}}
			e := &Engine{usage: u, docTools: allDocTools(), imageTools: []imagegen.Tool{imagegen.NewTool(fakeImageProvider{})}}
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

// Every docgen tool as offered widens the LLM client's tool-call idle window:
// a buffering endpoint goes silent mid-argument for longer than the normal
// window. A non-document tool keeps the normal window and stalls.
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

	e := &Engine{artifacts: fakeArtifactStore{}, usersDir: t.TempDir(), docTools: allDocTools()}
	offered := map[string]llm.Tool{}
	for _, tool := range e.availableTools(chat.Thread{}, newToolGate(string(classifier.Coding), "", "")) {
		offered[tool.Function.Name] = tool
	}
	for _, gen := range allDocTools() {
		tool, ok := offered[gen.ToolName()]
		if !ok {
			t.Fatalf("%s not offered", gen.ToolName())
		}
		if err := stream(tool); err != nil {
			t.Errorf("%s: stream error = %v, want the widened idle window", gen.ToolName(), err)
		}
	}
	if err := stream(offered[conversationSearchToolName]); !errors.Is(err, llm.ErrStreamStalled) {
		t.Fatalf("non-document tool: error = %v, want ErrStreamStalled", err)
	}
}
