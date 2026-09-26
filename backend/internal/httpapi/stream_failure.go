package httpapi

import (
	"context"
	"errors"
	"log/slog"

	"github.com/trick77/loom/internal/llm"
)

// streamCanceled reports whether a turn that ended in err was cancelled rather
// than failed. The error alone is not enough: sqlite-vec reports an interrupt
// as "SQL logic error: chunks iter error", which is no context.Canceled.
func streamCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || ctx.Err() != nil
}

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
		// The client only learns "stream failed"; this line is where why lives.
		slog.Error(label+" stream failed",
			"thread_id", threadID,
			"err", err,
			"content_bytes", len(result.Content),
			"reasoning_bytes", len(result.ReasoningContent))
		return "stream failed"
	}
}
