package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/trick77/loom/internal/llm"
)

// readThreadToolName is the built-in tool the model calls to load the full
// conversation of one of the user's past threads by id — typically a thread id
// surfaced by conversation_search. It complements read_project_threads (which
// reads every sibling in the current project) by fetching a single, arbitrary
// owned thread, so search hits are not snippet-terminal.
const readThreadToolName = "read_thread"

// readThreadTool is the schema advertised to the model.
func readThreadTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: readThreadToolName,
			Description: "Load the full conversation content of one of the user's past threads by its id — " +
				"for example a thread id returned by conversation_search. " +
				"Use it to read a prior conversation in full before answering, after search points you to a relevant thread. " +
				"Returns the thread's title, last-activity date, and its message content in chronological order.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"thread_id": map[string]any{
						"type":        "string",
						"description": "The id of the thread to read, as shown in a conversation_search result.",
					},
				},
				"required": []any{"thread_id"},
			},
		},
	}
}

// readThreadDigest loads one thread the caller owns and renders its transcript.
// GetThread is user-scoped, so a thread id that is not the caller's (or does not
// exist) returns ok=false and a plain not-found note — there is no cross-user
// leak. The whole project-summary byte budget is spent on this single thread,
// then capToolOutput is the final guard on the tool-result envelope.
func (s *server) readThreadDigest(ctx context.Context, userID, threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "tool failed: thread_id is required."
	}
	thread, ok, err := s.thread.GetThread(ctx, userID, threadID)
	if err != nil {
		slog.Warn("read_thread: get thread failed", "thread_id", threadID, "error", err)
		return "tool failed: could not load that thread: " + err.Error()
	}
	if !ok {
		return fmt.Sprintf("No thread with id %q exists in your history.", threadID)
	}

	byteBudget := s.projectSummaryTokenBudget * bytesPerToken

	var b strings.Builder
	fmt.Fprintf(&b, "=== Thread: %s ===\n", strings.TrimSpace(displayThreadTitle(thread)))
	b.WriteString(s.renderThreadDigest(ctx, userID, thread, byteBudget))
	return capToolOutput(b.String())
}
