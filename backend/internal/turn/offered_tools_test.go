package turn

import (
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// countingTools counts how often a turn reads the MCP tool list.
type countingTools struct {
	fakeMCPService
	lists *int
}

func (c countingTools) Tools() []llm.Tool {
	*c.lists++
	return c.fakeMCPService.Tools()
}

func (c countingTools) ToolsFor(active map[string]bool) []llm.Tool {
	*c.lists++
	return c.fakeMCPService.ToolsFor(active)
}

// A turn builds its MCP tool set once: the prompt's tool guidance and the
// tools the loop offers come from the same list.
func TestTurnBuildsMCPToolsOnce(t *testing.T) {
	lists := 0
	tool := llm.Tool{Type: "function", Function: llm.ToolFunction{Name: "peeq__search"}}
	svc := countingTools{fakeMCPService: fakeMCPService{
		ToolList:     []llm.Tool{tool},
		Instructions: map[string]string{"peeq": "Search peeq for videos."},
	}, lists: &lists}
	client := &fakeToolChatClient{Results: []llm.StreamResult{{Content: "Hi."}}}
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "T"}}
	out := runStoredTurn(t, Config{MCP: svc, LLM: client}, store, "hello")
	if out.err != nil {
		t.Fatal(out.err)
	}
	if lists != 1 {
		t.Errorf("MCP tool list read %d times, want 1", lists)
	}
	if len(client.Histories) == 0 {
		t.Fatal("no tool round")
	}
	guided := false
	for _, m := range client.Histories[0] {
		guided = guided || strings.Contains(m.Content, "Search peeq for videos.")
	}
	if !guided {
		t.Error("the offered server's guidance is not in the prompt")
	}
	tools := client.Tools[0]
	if len(tools) == 0 || tools[len(tools)-1].Function.Name != "peeq__search" {
		t.Errorf("tools offered = %v, want peeq__search last", tools)
	}
}
