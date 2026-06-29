package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// projectThreadsToolName is the built-in tool the model calls to read the other
// threads in the current project so it can summarize, compile, or compare across
// them. It is exposed only when the active thread belongs to a project (see
// availableTools), so a project-less thread never sees it.
const projectThreadsToolName = "read_project_threads"

// maxProjectSummaryThreads caps how many sibling threads the digest covers. The
// most recently active threads are kept; any older overflow is noted in the
// digest rather than silently dropped.
const maxProjectSummaryThreads = 50

// projectThreadsTool is the schema advertised to the model. The description is
// written so MiMo's inline tool-caller reliably fires on the natural phrasings a
// user reaches for ("summarize the threads in this project", "compile what we
// found across these conversations").
func projectThreadsTool() llm.Tool {
	return llm.Tool{
		Type: "function",
		Function: llm.ToolFunction{
			Name: projectThreadsToolName,
			Description: "Read the titles and conversation content of the OTHER threads in the current project. " +
				"Call this whenever the user asks to summarize, compile, compare, or gather information across the " +
				"threads or conversations in this project (for example \"summarize the threads in this project\"). " +
				"It returns each thread's title, last-activity time, and message content (most recent thread first). " +
				"After reading, synthesize a single answer with whatever emphasis the user asked for; do not rely on " +
				"the project memory digest alone, which is lossy.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

// projectThreadsDigest reads the active thread's sibling threads (non-archived,
// excluding the current thread) and renders a budgeted, model-readable digest of
// their content. Pure DB read — no inference — so it comfortably fits the tool
// execution window. The output is bounded in BYTES (per-thread share, plus a hard
// overall ceiling via capToolOutput as a final guard), so returning it without the
// generic byte cap re-truncating the tail is safe even for multibyte content.
func (s *server) projectThreadsDigest(ctx context.Context, userID string, thread chat.Thread) string {
	if thread.ProjectID == nil {
		return "This thread does not belong to a project, so there are no other project threads to read."
	}

	// Request one more than the cap (plus room for the current thread, which the
	// query also returns) so we can detect and report overflow.
	siblings, err := s.thread.ListThreads(ctx, userID, chat.ListThreadsOptions{
		ProjectID: thread.ProjectID,
		Limit:     maxProjectSummaryThreads + 2,
	})
	if err != nil {
		slog.Warn("read_project_threads: list threads failed", "project_id", *thread.ProjectID, "error", err)
		return "tool failed: could not list the project's threads: " + err.Error()
	}

	others := make([]chat.Thread, 0, len(siblings))
	for _, t := range siblings {
		if t.ID == thread.ID {
			continue
		}
		others = append(others, t)
	}
	if len(others) == 0 {
		return "This project has no other threads yet, so there is nothing to summarize across threads."
	}

	omitted := 0
	if len(others) > maxProjectSummaryThreads {
		omitted = len(others) - maxProjectSummaryThreads
		others = others[:maxProjectSummaryThreads]
	}

	// Byte budget for the whole digest, divided evenly across threads. When the even
	// share would fall below the floor, raise it to the floor but cap the thread
	// count so floor × threads can't blow the total budget — bounding the output
	// regardless of how many threads the project has.
	byteBudget := s.projectSummaryTokenBudget * bytesPerToken
	perThreadBytes := byteBudget / len(others)
	if perThreadBytes < minPerThreadDigestBytes {
		perThreadBytes = minPerThreadDigestBytes
		if maxByFloor := byteBudget / minPerThreadDigestBytes; maxByFloor >= 1 && len(others) > maxByFloor {
			omitted += len(others) - maxByFloor
			others = others[:maxByFloor]
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "The current project contains %d other thread(s). Their content follows, most recently active first. "+
		"Use it to answer the user's request, synthesizing across threads with the emphasis they asked for.\n", len(others))
	if omitted > 0 {
		fmt.Fprintf(&b, "(Note: %d older thread(s) were omitted to stay within budget; the most recently active are included.)\n", omitted)
	}

	for i, t := range others {
		fmt.Fprintf(&b, "\n=== Thread %d: %s ===\n", i+1, strings.TrimSpace(displayThreadTitle(t)))
		b.WriteString(s.renderThreadDigest(ctx, userID, t, perThreadBytes))
	}

	// Final hard guard: even though the per-thread byte shares bound the total by
	// construction, pass through the shared byte cap so the result can never exceed
	// the tool-result envelope. capToolOutput is byte-safe via truncateUTF8.
	return capToolOutput(b.String())
}
