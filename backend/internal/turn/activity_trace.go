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

// toolTitle is a tool's human-readable name from its MCP server, or "" for a
// built-in or an untitled tool.
func (s *Engine) toolTitle(name string) string {
	if s.mcp == nil {
		return ""
	}
	return s.mcp.ToolTitle(name)
}

// stampToolTitles records each tool event's server-given title in the
// persisted trace (flat and per block), so a reloaded thread describes its
// tool calls the way the live stream did.
func (s *Engine) stampToolTitles(trace []ActivityTraceEvent, blocks []ContentBlock) {
	stamp := func(events []ActivityTraceEvent) {
		for i := range events {
			if events[i].Type == "tool" && events[i].ToolTitle == "" {
				events[i].ToolTitle = s.toolTitle(events[i].Name)
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
