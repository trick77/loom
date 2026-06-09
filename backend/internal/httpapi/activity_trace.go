package httpapi

import (
	"fmt"
	"strings"

	"github.com/trick77/slopr/internal/llm"
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

// activityTraceFromResult appends a turn's reasoning and tool calls to the
// trace. It returns the id assigned to the reasoning event (or "" when the turn
// produced no reasoning) so the caller can attach a background-generated title.
func activityTraceFromResult(current []activityTraceEvent, result llm.StreamResult) ([]activityTraceEvent, string) {
	next := append([]activityTraceEvent(nil), current...)
	reasoningID := ""
	if strings.TrimSpace(result.ReasoningContent) != "" {
		reasoningID = fmt.Sprintf("reasoning-%d", countActivityTraceReasoning(next)+1)
		next = append(next, activityTraceEvent{
			ID:      reasoningID,
			Type:    "reasoning",
			Content: result.ReasoningContent,
			Status:  "done",
		})
	}
	for _, call := range result.ToolCalls {
		next = append(next, activityTraceEvent{
			ID:           call.ID,
			Type:         "tool",
			Name:         call.Function.Name,
			Status:       "running",
			RawArguments: call.Function.Arguments,
		})
	}
	return next, reasoningID
}

func activityTraceWithToolResult(current []activityTraceEvent, toolCallID, output string) []activityTraceEvent {
	next := append([]activityTraceEvent(nil), current...)
	for i := range next {
		if next[i].Type != "tool" || next[i].ID != toolCallID {
			continue
		}
		next[i].Status = "done"
		if strings.HasPrefix(output, toolFailedPrefix) {
			next[i].Status = "failed"
		}
		next[i].RawOutput = output
		return next
	}
	return next
}

// nextReasoningID is the id activityTraceFromResult will assign to the next
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
