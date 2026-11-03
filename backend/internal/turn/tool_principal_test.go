package turn

import (
	"context"
	"sync"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// Every MCP call of a turn carries the asking user, so a per-user server reads
// that user's own account; concurrent web reads included.
func TestTurnToolCallsCarryTheCaller(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{
			{ID: "c1", Function: llm.ToolCallFunction{Name: "tunewire__recently_played", Arguments: `{}`}},
			{ID: "c2", Function: llm.ToolCallFunction{Name: fetchToolName, Arguments: `{"url":"https://example.com"}`}},
		}},
		{Content: "Done."},
	}}
	var mu sync.Mutex
	seen := map[string]string{}
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{
				{Type: "function", Function: llm.ToolFunction{Name: "tunewire__recently_played"}},
				{Type: "function", Function: llm.ToolFunction{Name: fetchToolName}},
			},
			CallFunc: func(ctx context.Context, name string, _ map[string]any) (string, error) {
				p, _ := mcp.PrincipalFrom(ctx)
				mu.Lock()
				seen[name] = p.ID
				mu.Unlock()
				return "ok", nil
			},
		},
	}, store, "What did I play?")
	if out.err != nil {
		t.Fatalf("turn: %v", out.err)
	}
	for _, name := range []string{"tunewire__recently_played", fetchToolName} {
		if seen[name] != testUser.ID {
			t.Errorf("%s ran for %q, want %q", name, seen[name], testUser.ID)
		}
	}
}
