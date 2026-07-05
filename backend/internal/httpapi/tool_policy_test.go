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
		name     string
		category string
		message  string
		want     bool
	}{
		{"coding category enables docgen", string(classifier.Coding), "refactor this", true},
		{"general category alone does not", string(classifier.General), "hello there", false},
		{"weather category alone does not", string(classifier.Weather), "is it raining", false},
		{"english file ask escalates from general", string(classifier.General), "put that in a PDF", true},
		{"english spreadsheet ask escalates", string(classifier.General), "export it as a spreadsheet", true},
		{"german presentation ask escalates", string(classifier.General), "Erstelle eine Präsentation dazu", true},
		{"german spreadsheet ask escalates", string(classifier.General), "Mach mir eine Tabelle draus", true},
		{"german export ask escalates", string(classifier.General), "Kannst du das als Bericht exportieren?", true},
		{"german document ask escalates", string(classifier.Weather), "Fass das in einem Dokument zusammen", true},
		{"plain chit-chat stays off", string(classifier.General), "wie geht es dir heute", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := newToolGate(tt.category, tt.message)
			if got := gate.docgenEnabled(); got != tt.want {
				t.Fatalf("docgenEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToolGateActiveCategoriesCodingEscalation(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		message     string
		wantCoding  bool
		wantGeneral bool
	}{
		{"coding thread is active", string(classifier.Coding), "how do I do X", true, false},
		{"general chit-chat has no coding", string(classifier.General), "tell me a joke", false, true},
		{"code fence escalates coding from general", string(classifier.General), "why does ```go\nx := 1\n``` fail", true, true},
		{"npm signal escalates coding", string(classifier.General), "run npm install then build", true, true},
		{"german stacktrace escalates coding", string(classifier.General), "Ich habe einen Stacktrace bekommen", true, true},
		{"german compile signal escalates coding", string(classifier.Weather), "Das laesst sich nicht kompilieren", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active := newToolGate(tt.category, tt.message).activeCategories()
			if got := active[string(classifier.Coding)]; got != tt.wantCoding {
				t.Fatalf("active[coding] = %v, want %v", got, tt.wantCoding)
			}
			if got := active[tt.category]; got != true {
				t.Fatalf("active[%s] = %v, want true (base category always present)", tt.category, got)
			}
			_ = tt.wantGeneral
		})
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

	if hasCreateText(newToolGate(string(classifier.General), "just chatting")) {
		t.Fatal("docgen tools should be gated off for a general chit-chat turn")
	}
	if !hasCreateText(newToolGate(string(classifier.General), "save this as a pdf")) {
		t.Fatal("docgen tools should be present once the message asks for a file")
	}
	if !hasCreateText(newToolGate(string(classifier.WritingEditing), "draft a memo")) {
		t.Fatal("docgen tools should be present for a document-producing category")
	}
}

// TestAvailableToolsGatesCategoryTaggedMCP exercises the real
// availableTools -> ToolService.ToolsFor wiring: a coding-tagged MCP tool (e.g.
// context7) is withheld for a general turn and appears once the turn is coding —
// whether by category or by keyword escalation. This is the "context7 only when
// coding" headline behavior at the assembly seam.
func TestAvailableToolsGatesCategoryTaggedMCP(t *testing.T) {
	srv := &server{
		mcp: fakeMCPService{
			tools: []llm.Tool{
				{Type: "function", Function: llm.ToolFunction{Name: "tavily__search"}},
				{Type: "function", Function: llm.ToolFunction{Name: "context7__query-docs"}},
			},
			toolCategories: map[string][]string{
				"context7__query-docs": {string(classifier.Coding)},
			},
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

	generalGate := newToolGate(string(classifier.General), "hello there")
	if !has(generalGate, "tavily__search") {
		t.Fatal("category-neutral web search should always be offered")
	}
	if has(generalGate, "context7__query-docs") {
		t.Fatal("context7 should be gated off for a general turn")
	}
	if !has(newToolGate(string(classifier.Coding), "fix this bug"), "context7__query-docs") {
		t.Fatal("context7 should be offered for a coding-classified thread")
	}
	if !has(newToolGate(string(classifier.General), "why does ```go x:=1``` fail"), "context7__query-docs") {
		t.Fatal("context7 should be offered when the message escalates coding intent")
	}
}

// TestFileToolGuardrailTracksDocgenGating locks in the prompt<->tools consistency:
// the create_*_file guidance must appear in the system prompt only on turns where
// the docgen tools are actually offered. Naming a gated-out tool would invite the
// model to call a tool whose schema it never received.
func TestFileToolGuardrailTracksDocgenGating(t *testing.T) {
	user := auth.User{ID: "u1", ResponseLanguage: "auto"}
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

	if strings.Contains(systemPrompt(newToolGate(string(classifier.General), "just chatting")), "create_pdf_file") {
		t.Fatal("file-tool guardrail should be absent when docgen is gated off")
	}
	if !strings.Contains(systemPrompt(newToolGate(string(classifier.Coding), "help me")), "create_pdf_file") {
		t.Fatal("file-tool guardrail should be present when docgen is offered")
	}
	if !strings.Contains(systemPrompt(newToolGate(string(classifier.General), "export this as a pdf")), "create_pdf_file") {
		t.Fatal("file-tool guardrail should be present once a file is requested via escalation")
	}
}
