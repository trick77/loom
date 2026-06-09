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
	Name         string `json:"name,omitempty"`
	Status       string `json:"status"`
	RawArguments string `json:"rawArguments,omitempty"`
	RawOutput    string `json:"rawOutput,omitempty"`
}

func activityTraceFromResult(current []activityTraceEvent, result llm.StreamResult) []activityTraceEvent {
	next := append([]activityTraceEvent(nil), current...)
	if strings.TrimSpace(result.ReasoningContent) != "" {
		next = append(next, activityTraceEvent{
			ID:      fmt.Sprintf("reasoning-%d", countActivityTraceReasoning(next)+1),
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
	return next
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

func countActivityTraceReasoning(events []activityTraceEvent) int {
	count := 0
	for _, event := range events {
		if event.Type == "reasoning" {
			count++
		}
	}
	return count
}
