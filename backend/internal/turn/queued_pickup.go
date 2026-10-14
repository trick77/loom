package turn

import (
	"context"
	"log/slog"
	"strings"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/llm"
)

// pickUp is the outcome of a tool boundary's look at the inbox.
type pickUp struct {
	// split: the turn so far was stored as a segment, so the loop starts a
	// fresh block builder.
	split bool
	// history is the model history to continue with; nil keeps the current
	// one (nothing was picked up).
	history []llm.Message
}

// pickUpQueued runs at a tool boundary: after a round's tool results, before
// the next model call. When the user queued messages meanwhile, it stores the
// turn so far as a segment, moves the queued messages into the thread after
// it and returns the history to continue with, in the shape a later turn
// would read it back: the prefix, the segment as one assistant message
// carrying the gathered tool notes instead of raw tool rows, then the queued
// messages. A failure here never fails the turn: the messages stay queued
// for the follow-up.
func (t *Run) pickUpQueued(ctx context.Context, prefix, rounds []llm.Message, b *blockBuilder, artifacts []artifact.Response, sources []webSource) pickUp {
	if t.inbox == nil {
		return pickUp{}
	}
	has, err := t.inbox.HasQueued(ctx)
	if err != nil {
		slog.Warn("check queued messages failed", "thread_id", t.thread.ID, "err", err)
		return pickUp{}
	}
	if !has {
		return pickUp{}
	}
	// A stop arriving in the middle must not leave a segment without the
	// messages that caused it.
	ctx = context.WithoutCancel(ctx)
	if _, err := t.persistSegment(ctx, b, artifacts, sources); err != nil {
		slog.Warn("persist turn segment failed", "thread_id", t.thread.ID, "err", err)
		return pickUp{}
	}
	queued, err := t.inbox.Consume(ctx)
	if err != nil {
		slog.Warn("consume queued messages failed", "thread_id", t.thread.ID, "err", err)
		return pickUp{split: true}
	}
	if len(queued) == 0 {
		return pickUp{split: true}
	}
	next := prefix[:len(prefix):len(prefix)]
	if content := segmentHistoryContent(b.prose(), rounds); content != "" {
		next = append(next, llm.Message{Role: "assistant", Content: content})
	}
	for _, m := range queued {
		_ = t.stream.SendJSON("user_message", m)
		next = append(next, llm.Message{Role: "user", Content: m.Content})
	}
	slog.Info("picked up queued messages", "thread_id", t.thread.ID, "count", len(queued))
	return pickUp{split: true, history: next}
}

// segmentHistoryContent is the segment as the continuing model call sees it:
// its prose, then the notes of its tool rounds, so what the tools found is
// not lost with the raw rows.
func segmentHistoryContent(prose string, rounds []llm.Message) string {
	notes := collectToolNotes(rounds, finalSynthesisNotesBudgetTokens*bytesPerToken)
	parts := []string{}
	if strings.TrimSpace(prose) != "" {
		parts = append(parts, prose)
	}
	if notes != "" {
		parts = append(parts, "Research notes gathered so far:\n\n"+notes)
	}
	return strings.Join(parts, "\n\n---\n")
}
