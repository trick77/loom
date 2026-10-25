package turn

import (
	"strings"
	"testing"

	"github.com/trick77/loom/internal/llm"
)

// A server's guidance may word citations its own way ("cite the links given");
// loom's [n] rule closes the block so it is the last word on citing.
func TestMCPServerGuidanceEndsWithLoomCitationRule(t *testing.T) {
	tool := llm.Tool{Type: "function", Function: llm.ToolFunction{Name: "peeq__search"}}
	for _, c := range []struct {
		name         string
		instructions map[string]string
		want         bool
	}{
		{"server guidance", map[string]string{"peeq": "Cite results with the links given."}, true},
		{"no guidance", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := &Engine{mcp: fakeMCPService{ToolList: []llm.Tool{tool}, Instructions: c.instructions}}
			got := e.mcpServerGuidance(testUser, toolGate{})
			if strings.Contains(got, mcpCitationOverride) != c.want || (c.want && !strings.HasSuffix(got, mcpCitationOverride)) {
				t.Fatalf("want the override closing the guidance = %v:\n%s", c.want, got)
			}
		})
	}
}

// Both citation rules forbid a source's URL in the answer: the model once wrote
// "[https://…]" in place of the marker.
func TestCitationRulesForbidSourceURLs(t *testing.T) {
	if !strings.Contains(loomSystemPrompt, sourceURLRule) {
		t.Error("system prompt lacks the no-URL citation rule")
	}
	history := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "question"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1"}}},
		{Role: "tool", ToolCallID: "c1", Content: "Source [1]: A video — https://a.example\n\nnote"},
	}
	synth, ok := buildFinalSynthesisHistory(history, 2, "", []webSource{{Index: 1, URL: "https://a.example", Label: "A"}})
	if !ok || !strings.Contains(synth[len(synth)-1].Content, sourceURLRule) {
		t.Error("final synthesis instruction lacks the no-URL citation rule")
	}
}
