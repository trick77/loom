package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/sse"
)

// persistAssistantTurn turns a finished loop result into the stored assistant
// message: it offers the answer's large code blocks as downloadable files,
// waits for the background reasoning titles and stamps them onto the trace,
// merges the knowledge and web citations, and inserts the row. ctx must
// outlive the request (the caller detaches it): a client that disconnected
// still gets its answer persisted.
func (s *server) persistAssistantTurn(ctx context.Context, stream *sse.Writer, titles *reasoningTitleTracker, user auth.User, thread chat.Thread, result *assistantLoopResult, knowledgeSources []citation, usageTotal *llm.UsageAccumulator, turnStart time.Time) (chat.Message, error) {
	artifacts := result.Artifacts
	if artifacts == nil {
		artifacts = []artifactResponse{}
	}
	// Offer substantial inline code/XML blocks as downloadable files. This is a
	// deterministic, code-driven pass over the finished answer — the model emits
	// the code inline (and is told not to call file tools itself), and the size
	// gate here decides what becomes a download.
	if extracted := s.extractCodeArtifacts(ctx, stream, user, thread, result.Content, artifacts); len(extracted) > 0 {
		artifacts = append(artifacts, extracted...)
		// These artifacts are derived from the FINAL answer's inline code, so they
		// render after the final text: append them as artifact blocks at the END of
		// the timeline.
		for i := range extracted {
			result.Blocks = append(result.Blocks, contentBlock{Type: "artifact", Artifact: &extracted[i]})
		}
	}
	artifactsJSON, err := json.Marshal(artifacts)
	if err != nil {
		return chat.Message{}, fmt.Errorf("marshal artifacts: %w", err)
	}
	// Ensure every background title has landed and been emitted before persisting
	// the trace; this also guarantees the title SSE events precede assistant_message.
	titles.wait()
	titles.mergeInto(result.ActivityTrace)
	// The blocks' trace events are separate objects from the flat trace but share
	// reasoning ids, so stamp the same titles onto them too.
	titles.mergeIntoBlocks(result.Blocks)
	activityTraceJSON, contentBlocksJSON := marshalTurnJSON(thread.ID, result.ActivityTrace, result.Blocks)
	allSources := knowledgeSources
	if webCitations := webSourceCitations(result.WebSources); len(webCitations) > 0 {
		// Clone first: appending into knowledgeSources' backing array could clobber
		// it if it had spare capacity. It's not read after this today, but the copy
		// keeps the merge self-contained.
		allSources = append(append([]citation(nil), knowledgeSources...), webCitations...)
	}
	citationsJSON := json.RawMessage("[]")
	if len(allSources) > 0 {
		if encoded, marshalErr := json.Marshal(allSources); marshalErr == nil {
			citationsJSON = encoded
		}
	}
	turnCost, turnPriced := usageTotal.TurnCost()
	assistantMessage, err := s.thread.AddMessageWithCitations(ctx, user.ID, thread.ID, chat.RoleAssistant, result.Content, messageMetricsWithCost(result.StreamResult, usageTotal.Total(), time.Since(turnStart), turnCost, turnPriced), artifactsJSON, activityTraceJSON, citationsJSON, contentBlocksJSON)
	if err != nil {
		return chat.Message{}, err
	}
	return assistantMessage, nil
}

// marshalTurnJSON encodes a turn's activity trace and content blocks for the
// message row. Either failing is logged and stored as an empty array rather
// than failing the persist: the prose is what matters, the trace is
// decoration.
func marshalTurnJSON(threadID string, trace []activityTraceEvent, blocks []contentBlock) (traceJSON, blocksJSON []byte) {
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
