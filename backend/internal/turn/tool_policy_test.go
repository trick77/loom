package turn

import (
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/turn/turntest"
)

func TestToolGateDocgenEnabled(t *testing.T) {
	tests := []struct {
		name         string
		category     string
		turnCategory string
		message      string
		want         bool
	}{
		{"coding category enables docgen", string(classifier.Coding), "", "refactor this", true},
		{"general category alone does not", string(classifier.General), "", "hello there", false},
		{"weather category alone does not", string(classifier.Weather), "", "is it raining", false},
		{"fresh turn category enables docgen", string(classifier.General), string(classifier.WritingEditing), "polish this", true},
		{"empty category widens to all", "", "", "anything at all", true},
		{"english file ask escalates from general", string(classifier.General), "", "put that in a PDF", true},
		{"english spreadsheet ask escalates", string(classifier.General), "", "export it as a spreadsheet", true},
		{"german presentation ask escalates", string(classifier.General), "", "Erstelle eine Präsentation dazu", true},
		{"german spreadsheet ask escalates", string(classifier.General), "", "Mach mir eine Tabelle draus", true},
		{"german export ask escalates", string(classifier.General), "", "Kannst du das als Bericht exportieren?", true},
		{"german document ask escalates", string(classifier.Weather), "", "Fass das in einem Dokument zusammen", true},
		{"german word ask escalates", string(classifier.General), "", "Mach mir ein Word draus", true},
		{"plain chit-chat stays off", string(classifier.General), string(classifier.General), "wie geht es dir heute", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := newToolGate(tt.category, tt.turnCategory, tt.message)
			if got := gate.docgenEnabled(); got != tt.want {
				t.Fatalf("docgenEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolGateActiveCategoriesUnionsStickyAndTurn(t *testing.T) {
	// The active set is the union of the sticky and fresh-turn categories, so a
	// coding drift on a general thread activates the coding-tagged servers.
	active := newToolGate(string(classifier.General), string(classifier.Coding), "any message").activeCategories()
	if !active[string(classifier.General)] {
		t.Fatalf("active missing sticky category general: %v", active)
	}
	if !active[string(classifier.Coding)] {
		t.Fatalf("active missing drifted turn category coding: %v", active)
	}

	// No drift: only the sticky category is active.
	active = newToolGate(string(classifier.Weather), "", "will it rain").activeCategories()
	if active[string(classifier.Coding)] {
		t.Fatalf("weather turn should not activate coding: %v", active)
	}
	if !active[string(classifier.Weather)] {
		t.Fatalf("active missing sticky category weather: %v", active)
	}
}

func TestToolGateWidenAll(t *testing.T) {
	if !newToolGate("", "", "hi").widenAll() {
		t.Fatal("empty sticky + empty turn category should widen to all")
	}
	if newToolGate(string(classifier.General), "", "hi").widenAll() {
		t.Fatal("a known sticky category must not widen to all")
	}
	if newToolGate("", string(classifier.Coding), "hi").widenAll() {
		t.Fatal("a known turn category must not widen to all")
	}
}

func TestCategoryGrantsCodingDocs(t *testing.T) {
	for _, c := range []classifier.Category{classifier.Coding, classifier.HowTo} {
		if !categoryGrantsCodingDocs(string(c)) {
			t.Fatalf("%q should already grant coding-doc tools", c)
		}
	}
	for _, c := range []string{string(classifier.General), string(classifier.Weather), ""} {
		if categoryGrantsCodingDocs(c) {
			t.Fatalf("%q should not grant coding-doc tools (drift check must run)", c)
		}
	}
}

// TestAvailableToolsGatesDocgen verifies the end-to-end effect on the offered
// tool set: the document tools (any tool of the docgen server) are withheld
// for a plain category and present, long-running, once the gate enables them,
// and only where files can be stored and the server may make them.
func TestAvailableToolsGatesDocgen(t *testing.T) {
	docTools := fakeMCPService{
		ToolList:  []llm.Tool{turntest.DocgenToolDef("create_text_file"), turntest.DocgenToolDef("create_new_format")},
		Artifacts: turntest.DocgenArtifacts(),
	}
	srv := &Engine{artifacts: fakeArtifactStore{}, usersDir: t.TempDir(), mcp: docTools}

	offered := func(e *Engine, gate toolGate) []llm.Tool {
		var out []llm.Tool
		for _, tool := range e.availableTools(chat.Thread{}, gate) {
			if strings.HasPrefix(tool.Function.Name, "docgen__") {
				out = append(out, tool)
			}
		}
		return out
	}

	if got := offered(srv, newToolGate(string(classifier.General), string(classifier.General), "just chatting")); len(got) != 0 {
		t.Fatalf("docgen tools should be gated off for a general chit-chat turn: %v", got)
	}
	for name, gate := range map[string]toolGate{
		"file asked for":      newToolGate(string(classifier.General), "", "save this as a pdf"),
		"document category":   newToolGate(string(classifier.WritingEditing), "", "draft a memo"),
		"no category (widen)": newToolGate("", "", "anything"),
	} {
		got := offered(srv, gate)
		if len(got) != 2 || !got[0].LongRunning || !got[1].LongRunning {
			t.Fatalf("%s: offered %+v, want both docgen tools, long-running", name, got)
		}
	}
	nowhere := &Engine{mcp: docTools}
	if got := offered(nowhere, newToolGate("", "", "anything")); len(got) != 0 {
		t.Fatalf("docgen tools offered without artifact storage: %v", got)
	}
	// An mcp.json docgen without "artifacts": every file would be dropped.
	docTools.Artifacts = nil
	noArtifacts := &Engine{artifacts: fakeArtifactStore{}, usersDir: t.TempDir(), mcp: docTools}
	if got := offered(noArtifacts, newToolGate("", "", "anything")); len(got) != 0 {
		t.Fatalf("docgen tools offered from a server without artifacts: %v", got)
	}
}

// TestAvailableToolsGatesCategoryTaggedMCP exercises the real
// availableTools -> ToolService.ToolsFor wiring: a coding-tagged MCP tool (e.g.
// context7) is withheld for a general turn, appears once a fresh classification
// judges the turn coding, and is always present when there is no category signal.
func TestAvailableToolsGatesCategoryTaggedMCP(t *testing.T) {
	srv := &Engine{
		mcp: fakeMCPService{
			ToolList: []llm.Tool{
				{Type: "function", Function: llm.ToolFunction{Name: "search__web"}},
				{Type: "function", Function: llm.ToolFunction{Name: "context7__query-docs"}},
			},
			ToolCategories: map[string][]string{"context7__query-docs": {string(classifier.Coding)}},
		},
	}

	has := func(gate toolGate, name string) bool {
		for _, tool := range srv.availableTools(chat.Thread{}, gate) {
			if tool.Function.Name == name {
				return true
			}
		}
		return false
	}

	generalGate := newToolGate(string(classifier.General), string(classifier.General), "hello there")
	if !has(generalGate, "search__web") {
		t.Fatal("category-neutral web search should always be offered")
	}
	if has(generalGate, "context7__query-docs") {
		t.Fatal("context7 should be gated off for a general turn")
	}
	if !has(newToolGate(string(classifier.Coding), "", "fix this bug"), "context7__query-docs") {
		t.Fatal("context7 should be offered for a coding-classified thread")
	}
	if !has(newToolGate(string(classifier.General), string(classifier.Coding), "any message"), "context7__query-docs") {
		t.Fatal("context7 should be offered when a fresh classification drifts to coding")
	}
	if !has(newToolGate("", "", "any message"), "context7__query-docs") {
		t.Fatal("context7 should be offered when there is no category signal (widen-all)")
	}
}

// TestFileToolGuardrailTracksOfferedDocumentTools locks in the prompt<->tools
// consistency through a whole turn: the file-tool guidance is in the system
// prompt only when a document tool is actually offered. Naming one the gate
// left out, or whose sidecar is down, would invite the model to call a tool
// whose schema it never received.
func TestFileToolGuardrailTracksOfferedDocumentTools(t *testing.T) {
	systemPrompt := func(t *testing.T, category, content string, tools ...llm.Tool) string {
		t.Helper()
		llmClient := &fakeToolChatClient{ClassifyResult: category, TitleResult: "T", Results: []llm.StreamResult{{Content: "ok"}}}
		out := runStoredTurn(t, Config{
			Artifacts: fakeArtifactStore{},
			UsersDir:  t.TempDir(),
			LLM:       llmClient,
			MCP: fakeMCPService{
				ToolList:  append([]llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}}, tools...),
				Artifacts: turntest.DocgenArtifacts(),
			},
		}, &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle}}, content)
		if out.err != nil || len(llmClient.Histories) == 0 {
			t.Fatalf("turn: %v, %d rounds; body=%s", out.err, len(llmClient.Histories), out.body)
		}
		return llmClient.Histories[0][0].Content
	}
	doc := turntest.DocgenToolDef("create_pdf_file")

	if !strings.Contains(systemPrompt(t, string(classifier.Coding), "help me", doc), fileToolGuardrailPrompt) {
		t.Fatal("file-tool guardrail should be present when a document tool is offered")
	}
	if !strings.Contains(fileToolGuardrailPrompt, "docgen__*") {
		t.Fatalf("guardrail names no document tool: %q", fileToolGuardrailPrompt)
	}
	if strings.Contains(systemPrompt(t, string(classifier.General), "just chatting", doc), fileToolGuardrailPrompt) {
		t.Fatal("file-tool guardrail should be absent when docgen is gated off")
	}
	if strings.Contains(systemPrompt(t, string(classifier.Coding), "help me"), fileToolGuardrailPrompt) {
		t.Fatal("file-tool guardrail should be absent while no document tool is served")
	}
}
