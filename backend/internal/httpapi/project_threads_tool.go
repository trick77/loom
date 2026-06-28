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

// minPerThreadDigestTokens floors the per-thread share so that, with many sibling
// threads, each one still contributes enough to carry its conclusion. The total
// digest may then exceed projectSummaryTokenBudget on very large projects; that
// is an accepted trade (coverage over a hard cap) and stays bounded by the thread
// cap above.
const minPerThreadDigestTokens = 150

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
// execution window. The output is returned verbatim (not run through
// capToolOutput): its size is bounded by the per-thread token budget instead, so
// the generic 32KB cap can't re-truncate the tail.
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

	perThreadBudget := s.projectSummaryTokenBudget / len(others)
	if perThreadBudget < minPerThreadDigestTokens {
		perThreadBudget = minPerThreadDigestTokens
	}

	var b strings.Builder
	fmt.Fprintf(&b, "The current project contains %d other thread(s). Their content follows, most recently active first. "+
		"Use it to answer the user's request, synthesizing across threads with the emphasis they asked for.\n", len(others))
	if omitted > 0 {
		fmt.Fprintf(&b, "(Note: %d older thread(s) were omitted to stay within budget; the most recently active are included.)\n", omitted)
	}

	for i, t := range others {
		fmt.Fprintf(&b, "\n=== Thread %d: %s ===\n", i+1, strings.TrimSpace(displayThreadTitle(t)))
		if t.LastMessageAt != nil {
			fmt.Fprintf(&b, "Last activity: %s\n", t.LastMessageAt.Format("2006-01-02"))
		}
		messages, _, err := s.thread.ListMessages(ctx, userID, t.ID)
		if err != nil {
			slog.Warn("read_project_threads: list messages failed", "thread_id", t.ID, "error", err)
			b.WriteString("(could not load this thread's messages)\n")
			continue
		}
		section := buildThreadDigestSection(messages, perThreadBudget)
		if section == "" {
			b.WriteString("(no readable messages in this thread)\n")
			continue
		}
		b.WriteString(section)
	}

	return b.String()
}

// displayThreadTitle returns a non-empty label for a thread.
func displayThreadTitle(t chat.Thread) string {
	if title := strings.TrimSpace(t.Title); title != "" {
		return title
	}
	return "(untitled thread)"
}

// buildThreadDigestSection renders one thread's user/assistant turns within a
// per-thread token budget, last-message-first: research threads put the answer at
// the END, so we always keep the final turn and backfill earlier turns until the
// budget is hit. This guarantees each thread's conclusion survives even a tight
// budget, instead of front-truncating and keeping only the opening question. The
// kept turns are rendered back in chronological order for readability.
func buildThreadDigestSection(messages []chat.Message, tokenBudget int) string {
	type turn struct {
		role string
		text string
	}
	var kept []turn
	used := 0
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role == chat.RoleTool {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		label := roleLabel(m.Role)
		cost := estimateDigestTokens(label) + estimateDigestTokens(text)
		if used+cost > tokenBudget {
			if len(kept) == 0 {
				// The final substantive turn alone exceeds the budget. Keep its
				// tail (the conclusion) rather than dropping the whole thread.
				kept = append(kept, turn{role: label, text: truncateTailToTokens(text, tokenBudget)})
			}
			break
		}
		kept = append(kept, turn{role: label, text: text})
		used += cost
	}
	if len(kept) == 0 {
		return ""
	}
	var b strings.Builder
	for i := len(kept) - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "%s: %s\n", kept[i].role, kept[i].text)
	}
	return b.String()
}

// estimateDigestTokens approximates a token count with the same rune/4 heuristic
// the RAG package uses (its estimateTokens is unexported), avoiding a runtime
// tokenizer dependency.
func estimateDigestTokens(s string) int {
	n := len([]rune(s))
	return (n + 3) / 4
}

// truncateTailToTokens keeps roughly the last tokenBudget tokens of s (the
// conclusion of an answer), prefixing an ellipsis when content was dropped.
func truncateTailToTokens(s string, tokenBudget int) string {
	runes := []rune(s)
	maxRunes := tokenBudget * 4
	if len(runes) <= maxRunes {
		return s
	}
	return "…" + string(runes[len(runes)-maxRunes:])
}
