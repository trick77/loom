package httpapi

import (
	"fmt"

	"github.com/trick77/loom/internal/llm"
)

type activityTraceEvent struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Content      string `json:"content,omitempty"`
	Title        string `json:"title,omitempty"`
	Name         string `json:"name,omitempty"`
	Status       string `json:"status"`
	RawArguments string `json:"rawArguments,omitempty"`
	RawOutput    string `json:"rawOutput,omitempty"`
}

// reasoningEvent builds the trace event for a turn's reasoning content.
func reasoningEvent(id, content string) activityTraceEvent {
	return activityTraceEvent{
		ID:      id,
		Type:    "reasoning",
		Content: content,
		Status:  "done",
	}
}

// toolCallEvent builds the (initially running) trace event for a tool call.
func toolCallEvent(call llm.ToolCall) activityTraceEvent {
	return activityTraceEvent{
		ID:           call.ID,
		Type:         "tool",
		Name:         call.Function.Name,
		Status:       "running",
		RawArguments: call.Function.Arguments,
	}
}

// nextReasoningID is the id blockBuilder.addResult will assign to the next
// reasoning event appended to this trace. Computed up front so a title can be
// spawned at the reasoning->content boundary mid-turn, before the turn returns.
func nextReasoningID(events []activityTraceEvent) string {
	return fmt.Sprintf("reasoning-%d", countActivityTraceReasoning(events)+1)
}

func countActivityTraceReasoning(events []activityTraceEvent) int {
	count := 0
	for _, event := range events {
		if event.Type == "reasoning" {
			count++
		}
	}
	return count
}
