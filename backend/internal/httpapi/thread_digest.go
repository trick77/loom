package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/trick77/loom/internal/chat"
)

// maxRecentMessagesPerThread bounds how many of a thread's most recent messages
// are loaded for a digest. buildThreadDigestSection only ever keeps the tail that
// fits the byte budget, so loading the whole transcript (potentially hundreds of
// messages with large tool-result blobs) would be wasted work — this ceiling
// keeps the read cheap while still covering the final turns the digest needs.
const maxRecentMessagesPerThread = 50

// bytesPerToken converts a configured token budget into a byte budget. Digest
// size is bounded in BYTES (not runes) because the thing it bypasses —
// capToolOutput — and the model's context envelope are measured in bytes; a
// rune-based bound would under-count multibyte (CJK/emoji) content by up to ~4x.
const bytesPerToken = 4

// minPerThreadDigestBytes floors a per-thread share so each thread still carries
// its conclusion. To keep a multi-thread TOTAL bounded, the thread count is
// capped so floor × threads cannot exceed the byte budget (see projectThreadsDigest).
const minPerThreadDigestBytes = 600

// renderThreadDigest renders one owned thread's "Last activity" line followed by
// its recent user/assistant turns within byteBudget, last-turn-first selection
// rendered back in chronological order. Shared by read_project_threads (one call
// per sibling) and read_thread (a single call) — each prints its own "=== … ==="
// title line first, then delegates the activity header + transcript here so that
// rendering lives in one place. It never returns an error — the output feeds a
// tool result — surfacing load failures and empty threads as readable notes.
func (s *server) renderThreadDigest(ctx context.Context, userID string, t chat.Thread, byteBudget int) string {
	var b strings.Builder
	if t.LastMessageAt != nil {
		fmt.Fprintf(&b, "Last activity: %s\n", t.LastMessageAt.Format("2006-01-02"))
	}
	messages, err := s.thread.ListRecentMessages(ctx, userID, t.ID, maxRecentMessagesPerThread)
	if err != nil {
		slog.Warn("thread digest: list messages failed", "thread_id", t.ID, "error", err)
		b.WriteString("(could not load this thread's messages)\n")
		return b.String()
	}
	section := buildThreadDigestSection(messages, byteBudget)
	if section == "" {
		b.WriteString("(no readable messages in this thread)\n")
		return b.String()
	}
	b.WriteString(section)
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
// per-thread BYTE budget, last-message-first: research threads put the answer at
// the END, so we always keep the final turn and backfill earlier turns until the
// budget is hit. This guarantees each thread's conclusion survives even a tight
// budget, instead of front-truncating and keeping only the opening question. The
// kept turns are rendered back in chronological order for readability.
func buildThreadDigestSection(messages []chat.Message, byteBudget int) string {
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
		cost := len(label) + len(text) + len(": \n")
		if used+cost > byteBudget {
			if len(kept) == 0 {
				// The final substantive turn alone exceeds the budget. Keep its
				// tail (the conclusion) rather than dropping the whole thread.
				kept = append(kept, turn{role: label, text: truncateTailToBytes(text, byteBudget)})
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

// truncateTailToBytes keeps the last whole runes of s that fit in byteBudget bytes
// (the conclusion of an answer), prefixing an ellipsis when content was dropped.
// Rune-safe: it never splits a multibyte character.
func truncateTailToBytes(s string, byteBudget int) string {
	if len(s) <= byteBudget {
		return s
	}
	const ellipsis = "…"
	avail := byteBudget - len(ellipsis)
	if avail < 0 {
		avail = 0
	}
	runes := []rune(s)
	bytes := 0
	start := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		rb := utf8.RuneLen(runes[i])
		if bytes+rb > avail {
			break
		}
		bytes += rb
		start = i
	}
	return ellipsis + string(runes[start:])
}
