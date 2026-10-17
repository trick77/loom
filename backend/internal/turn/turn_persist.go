package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/chat"
)

// PersistAssistantTurn turns a finished loop result into the stored assistant
// message: it offers the answer's large code blocks as downloadable files,
// waits for the background reasoning titles and stamps them onto the trace,
// merges the knowledge and web citations, and inserts the row. ctx must
// outlive the request (the caller detaches it): a client that disconnected
// still gets its answer persisted.
func (t *Run) PersistAssistantTurn(ctx context.Context, result *LoopResult) (chat.Message, error) {
	artifacts := result.Artifacts
	if artifacts == nil {
		artifacts = []artifact.Response{}
	}
	// Offer substantial inline code/XML blocks as downloadable files. This is a
	// deterministic, code-driven pass over the finished answer — the model emits
	// the code inline (and is told not to call file tools itself), and the size
	// gate here decides what becomes a download.
	if extracted := t.extractCodeArtifacts(ctx, result.Content, artifacts); len(extracted) > 0 {
		artifacts = append(artifacts, extracted...)
		// These artifacts are derived from the FINAL answer's inline code, so they
		// render after the final text: append them as artifact blocks at the END of
		// the timeline.
		for i := range extracted {
			result.Blocks = append(result.Blocks, ContentBlock{Type: "artifact", Artifact: &extracted[i]})
		}
	}
	artifactsJSON, err := json.Marshal(artifacts)
	if err != nil {
		return chat.Message{}, fmt.Errorf("marshal artifacts: %w", err)
	}
	// Ensure every background title has landed and been emitted before persisting
	// the trace; this also guarantees the title SSE events precede assistant_message.
	t.titles.Wait()
	t.titles.MergeInto(result.ActivityTrace)
	// The blocks' trace events are separate objects from the flat trace but share
	// reasoning ids, so stamp the same titles onto them too.
	t.titles.MergeIntoBlocks(result.Blocks)
	t.stampToolTitles(result.ActivityTrace, result.Blocks)
	activityTraceJSON, contentBlocksJSON := MarshalTurnJSON(t.thread.ID, result.ActivityTrace, result.Blocks)
	turnCost, turnPriced := t.acc.TurnCost()
	assistantMessage, err := t.e.thread.AddMessageWithCitations(ctx, t.user.ID, t.thread.ID, chat.RoleAssistant, result.Content, MessageMetricsWithCost(result.StreamResult, t.acc.Total(), time.Since(t.start), turnCost, turnPriced), artifactsJSON, activityTraceJSON, t.citationsJSON(result.WebSources), contentBlocksJSON)
	if err != nil {
		return chat.Message{}, err
	}
	return assistantMessage, nil
}

// persistSegment stores what the turn produced so far as an assistant message
// of its own and emits it as assistant_segment: the turn is about to pick up
// a queued message, which goes after it in the transcript. The segment books
// no tokens or cost; the turn's whole spend stays on its final message.
func (t *Run) persistSegment(ctx context.Context, b *blockBuilder, artifacts []artifact.Response, sources []webSource) (chat.Message, error) {
	if artifacts == nil {
		artifacts = []artifact.Response{}
	}
	artifactsJSON, err := json.Marshal(artifacts)
	if err != nil {
		return chat.Message{}, fmt.Errorf("marshal artifacts: %w", err)
	}
	t.titles.Wait()
	trace := b.flatTrace()
	t.titles.MergeInto(trace)
	t.titles.MergeIntoBlocks(b.blocks)
	t.stampToolTitles(trace, b.blocks)
	traceJSON, blocksJSON := MarshalTurnJSON(t.thread.ID, trace, b.blocks)
	segment, err := t.e.thread.AddMessageWithCitations(ctx, t.user.ID, t.thread.ID, chat.RoleAssistant, b.prose(), chat.MessageTokenUsage{}, artifactsJSON, traceJSON, t.citationsJSON(sources), blocksJSON)
	if err != nil {
		return chat.Message{}, err
	}
	if err := t.stream.SendJSON("assistant_segment", segment); err != nil {
		return chat.Message{}, err
	}
	return segment, nil
}

// citationsJSON merges the turn's knowledge sources and the given web sources
// into the message's citations column.
func (t *Run) citationsJSON(web []webSource) json.RawMessage {
	allSources := t.plan.knowledgeSources
	if webCitations := webSourceCitations(web); len(webCitations) > 0 {
		// Clone first: appending into the knowledge sources' backing array could
		// clobber it if it had spare capacity.
		allSources = append(append([]Citation(nil), t.plan.knowledgeSources...), webCitations...)
	}
	if len(allSources) > 0 {
		if encoded, err := json.Marshal(allSources); err == nil {
			return encoded
		}
	}
	return json.RawMessage("[]")
}

// MarshalTurnJSON encodes a turn's activity trace and content blocks for the
// message row. Either failing is logged and stored as an empty array rather
// than failing the persist: the prose is what matters, the trace is
// decoration.
func MarshalTurnJSON(threadID string, trace []ActivityTraceEvent, blocks []ContentBlock) (traceJSON, blocksJSON []byte) {
	traceJSON = []byte("[]")
	if encoded, err := json.Marshal(trace); err != nil {
		slog.Warn("marshal activity trace failed", "thread_id", threadID, "err", err)
	} else if trace != nil {
		traceJSON = encoded
	}
	blocksJSON = []byte("[]")
	if len(blocks) > 0 {
		if encoded, err := json.Marshal(blocks); err != nil {
			slog.Warn("marshal content blocks failed", "thread_id", threadID, "err", err)
		} else {
			blocksJSON = encoded
		}
	}
	return traceJSON, blocksJSON
}
