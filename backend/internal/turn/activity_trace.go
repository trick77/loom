package turn

import (
	"github.com/trick77/loom/internal/llm"
)

// ActivityTraceEvent is one step of a turn's persisted activity trace: a
// reasoning round or a tool call.
type ActivityTraceEvent struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Content      string `json:"content,omitempty"`
	Title        string `json:"title,omitempty"`
	Name         string `json:"name,omitempty"`
	Status       string `json:"status"`
	RawArguments string `json:"rawArguments,omitempty"`
	RawOutput    string `json:"rawOutput,omitempty"`
	// ToolTitle is a tool event's human-readable tool name from its MCP
	// server (Title is the reasoning title). Stamped at persist time.
	ToolTitle string `json:"toolTitle,omitempty"`
}

// reasoningEvent builds the trace event for a turn's reasoning content.
func reasoningEvent(id, content string) ActivityTraceEvent {
	return ActivityTraceEvent{
		ID:      id,
		Type:    "reasoning",
		Content: content,
		Status:  "done",
	}
}

// toolCallEvent builds the (initially running) trace event for a tool call.
func toolCallEvent(call llm.ToolCall) ActivityTraceEvent {
	return ActivityTraceEvent{
		ID:           call.ID,
		Type:         "tool",
		Name:         call.Function.Name,
		Status:       "running",
		RawArguments: call.Function.Arguments,
	}
}

// toolTitle is the human-readable name a tool call is streamed with: its MCP
// server's title, or "" for a built-in, an untitled tool, or a tool of a
// server limited to other accounts (its titles are that server's metadata
// too). The answer is remembered by call id for the persisted trace.
func (t *Run) toolTitle(callID, name string) string {
	if title, seen := t.toolTitles[callID]; seen && callID != "" {
		return title
	}
	title := ""
	if t.e.mcp != nil && t.e.mcp.ToolAllowed(MCPPrincipal(t.user), name) {
		title = t.e.mcp.ToolTitle(name)
	}
	if callID != "" {
		if t.toolTitles == nil {
			t.toolTitles = map[string]string{}
		}
		t.toolTitles[callID] = title
	}
	return title
}

// stampToolTitles records in the persisted trace (flat and per block) the
// title each tool call was streamed with, so a reloaded thread describes its
// tool calls exactly the way the live stream did.
func (t *Run) stampToolTitles(trace []ActivityTraceEvent, blocks []ContentBlock) {
	stamp := func(events []ActivityTraceEvent) {
		for i := range events {
			if events[i].Type == "tool" && events[i].ToolTitle == "" {
				events[i].ToolTitle = t.toolTitles[events[i].ID]
			}
		}
	}
	stamp(trace)
	for i := range blocks {
		stamp(blocks[i].Events)
	}
}

func countActivityTraceReasoning(events []ActivityTraceEvent) int {
	count := 0
	for _, event := range events {
		if event.Type == "reasoning" {
			count++
		}
	}
	return count
}
