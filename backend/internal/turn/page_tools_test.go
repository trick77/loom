package turn

import (
	"context"
	"slices"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/weather"
)

// The weather server's reverse lookup is the start page's: a turn never
// offers it, the rest of the server stays.
func TestPageOnlyToolsAreNeverOffered(t *testing.T) {
	tool := func(name string) llm.Tool {
		return llm.Tool{Type: "function", Function: llm.ToolFunction{Name: name}}
	}
	e := &Engine{mcp: fakeMCPService{ToolList: []llm.Tool{tool(weather.ForecastTool), tool(weather.ReverseGeocodeTool)}}}
	names := orderedToolNames(e.offeredMCPTools(testUser, toolGate{}))
	if !slices.Contains(names, weather.ForecastTool) || slices.Contains(names, weather.ReverseGeocodeTool) {
		t.Fatalf("offered = %v", names)
	}
}

// Named unprompted, it still never runs: its calls would spend the map
// provider's allowance the page needs.
func TestPageOnlyToolNeverRuns(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.ToolCallFunction{Name: weather.ReverseGeocodeTool, Arguments: `{"latitude":47,"longitude":8}`}}}},
		{Content: "Done."},
	}}
	var calls []string
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: weather.ReverseGeocodeTool}}},
			CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
				calls = append(calls, name)
				return `{"places":[]}`, nil
			},
		},
	}, store, "where is 47, 8?")
	if out.err != nil {
		t.Fatalf("turn error: %v", out.err)
	}
	if len(calls) != 0 {
		t.Fatalf("the page's tool ran in a turn: %v", calls)
	}
}
