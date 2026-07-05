package httpapi

import (
	"strings"
	"testing"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/llm"
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
// tool set: docgen schemas are withheld for a plain category and present once the
// gate enables them.
func TestAvailableToolsGatesDocgen(t *testing.T) {
	srv := &server{
		artifacts: fakeArtifactStore{},
		usersDir:  t.TempDir(),
		docTools:  []docgen.Generator{docgen.TextGenerator{}},
		mcp:       fakeMCPService{},
	}

	hasCreateText := func(gate toolGate) bool {
		for _, tool := range srv.availableTools(chat.Thread{}, gate) {
			if tool.Function.Name == "create_text_file" {
				return true
			}
		}
		return false
	}

	if hasCreateText(newToolGate(string(classifier.General), string(classifier.General), "just chatting")) {
		t.Fatal("docgen tools should be gated off for a general chit-chat turn")
	}
	if !hasCreateText(newToolGate(string(classifier.General), "", "save this as a pdf")) {
		t.Fatal("docgen tools should be present once the message asks for a file")
	}
	if !hasCreateText(newToolGate(string(classifier.WritingEditing), "", "draft a memo")) {
		t.Fatal("docgen tools should be present for a document-producing category")
	}
	if !hasCreateText(newToolGate("", "", "anything")) {
		t.Fatal("docgen tools should be present when there is no category signal (widen-all)")
	}
}

// TestAvailableToolsGatesCategoryTaggedMCP exercises the real
// availableTools -> ToolService.ToolsFor wiring: a coding-tagged MCP tool (e.g.
// context7) is withheld for a general turn, appears once a fresh classification
// judges the turn coding, and is always present when there is no category signal.
func TestAvailableToolsGatesCategoryTaggedMCP(t *testing.T) {
	srv := &server{
		mcp: fakeMCPService{
			tools: []llm.Tool{
				{Type: "function", Function: llm.ToolFunction{Name: "tavily__search"}},
				{Type: "function", Function: llm.ToolFunction{Name: "context7__query-docs"}},
			},
			toolCategories: map[string][]string{"context7__query-docs": {string(classifier.Coding)}},
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
	if !has(generalGate, "tavily__search") {
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

// TestFileToolGuardrailTracksDocgenGating locks in the prompt<->tools consistency:
// the create_*_file guidance must appear in the system prompt only on turns where
// the docgen tools are actually offered. Naming a gated-out tool would invite the
// model to call a tool whose schema it never received.
func TestFileToolGuardrailTracksDocgenGating(t *testing.T) {
	user := auth.User{ID: "u1", ResponseLanguage: "en"}
	newMsg := chat.Message{Role: chat.RoleUser, Content: "hello"}

	guidanceFor := func(gate toolGate) string {
		if gate.docgenEnabled() {
			return fileToolGuardrailPrompt
		}
		return ""
	}
	systemPrompt := func(gate toolGate) string {
		history := buildLLMHistory(user, guidanceFor(gate), "", "", "", "", "", nil, newMsg)
		return history[0].Content
	}

	if strings.Contains(systemPrompt(newToolGate(string(classifier.General), string(classifier.General), "just chatting")), "create_pdf_file") {
		t.Fatal("file-tool guardrail should be absent when docgen is gated off")
	}
	if !strings.Contains(systemPrompt(newToolGate(string(classifier.Coding), "", "help me")), "create_pdf_file") {
		t.Fatal("file-tool guardrail should be present when docgen is offered")
	}
	if !strings.Contains(systemPrompt(newToolGate(string(classifier.General), "", "export this as a pdf")), "create_pdf_file") {
		t.Fatal("file-tool guardrail should be present once a file is requested via escalation")
	}
}
