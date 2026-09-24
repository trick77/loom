package httpapi

import (
	"errors"
	"log/slog"

	"github.com/trick77/loom/internal/llm"
)

// streamFailureMessage maps a failed (not cancelled) assistant turn to the
// text of the error event. A streamUserError carries its own user-facing
// text; a stalled upstream is named as such and logged distinctly, since a
// recurring stall should be visible in the request logs; anything else is the
// generic failure. label names the handler in the log line.
func streamFailureMessage(err error, result assistantLoopResult, label, threadID string) string {
	var userErr streamUserError
	switch {
	case errors.As(err, &userErr):
		return userErr.message
	case errors.Is(err, llm.ErrStreamStalled):
		slog.Warn(label+" stream stalled",
			"thread_id", threadID,
			"content_bytes", len(result.Content),
			"reasoning_bytes", len(result.ReasoningContent))
		return llm.ErrStreamStalled.Error()
	default:
		return "stream failed"
	}
}
