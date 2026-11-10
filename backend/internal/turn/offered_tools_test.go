package turn

import (
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/turn/turntest"
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

// The tool guidance speaks only of servers the turn finally offers: a
// scheduled run drops the document tools (allowTools), so their server's
// guidance and the file-tool guidance stay out of its prompt too.
func TestGuidanceFollowsTheFinalOffer(t *testing.T) {
	svc := fakeMCPService{
		ToolList:     append([]llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "peeq__search"}}}, docToolDefs()...),
		Artifacts:    turntest.DocgenArtifacts(),
		Instructions: map[string]string{"peeq": "Search peeq.", mcp.DocgenServer: "Make files."},
	}
	e := &Engine{mcp: svc, artifacts: fakeArtifactStore{}, usersDir: t.TempDir()}
	gate := newToolGate(string(classifier.WritingEditing), "", "")
	for _, c := range []struct {
		name      string
		scheduled *ScheduledRun
		docs      bool
	}{
		{"interactive", nil, true},
		{"scheduled", &ScheduledRun{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			run := &Run{e: e, user: testUser, scheduled: c.scheduled}
			guidance := e.mcpServerGuidance(run.offer(gate), run.offered)
			if !strings.Contains(guidance, "Search peeq.") {
				t.Error("offered server's guidance missing")
			}
			if strings.Contains(guidance, "Make files.") != c.docs || (len(run.offeredDocTools) > 0) != c.docs {
				t.Errorf("document guidance %v, document tools %v, want both %v", strings.Contains(guidance, "Make files."), run.offeredDocTools, c.docs)
			}
		})
	}
}

// A turn reads the MCP tool list once: Prepare builds the offer, the prompt's
// tool guidance and every round read it (it was read 6 times per turn).
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
