package httpapi

import (
	"strings"

	"github.com/trick77/lume/internal/llm"
)

// contentBlock is one item in a message's ordered, interleaved timeline. Exactly
// one of the type-specific fields is populated per block, keyed by Type:
//   - "text":     Content (markdown prose)
//   - "trace":    Events (a contiguous run of reasoning+tool events = one panel)
//   - "artifact": Artifact (a produced artifact)
type contentBlock struct {
	Type     string               `json:"type"`
	Content  string               `json:"content,omitempty"`
	Events   []activityTraceEvent `json:"events,omitempty"`
	Artifact *artifactResponse    `json:"artifact,omitempty"`
}

// blockBuilder accumulates the chronological content blocks of a single
// assistant turn. Consecutive reasoning/tool events merge into the trailing
// trace block (one collapsible panel); a prose text block breaks that run.
type blockBuilder struct {
	blocks []contentBlock
}

// addTraceEvent appends a reasoning or tool event to the trailing trace block,
// creating a new trace block when the last block is not a trace run. This merges
// consecutive trace events (e.g. a round's reasoning then its tool calls) into a
// single panel, while a prose text block in between starts a fresh run.
func (b *blockBuilder) addTraceEvent(e activityTraceEvent) {
	if n := len(b.blocks); n > 0 && b.blocks[n-1].Type == "trace" {
		b.blocks[n-1].Events = append(b.blocks[n-1].Events, e)
		return
	}
	b.blocks = append(b.blocks, contentBlock{Type: "trace", Events: []activityTraceEvent{e}})
}

// addText appends a prose text block, preserving the original (untrimmed) string.
// Blank/whitespace-only content is skipped so empty rounds don't break the trace
// run with an empty panel separator.
func (b *blockBuilder) addText(s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	b.blocks = append(b.blocks, contentBlock{Type: "text", Content: s})
}

// addArtifact appends an artifact block at the position the artifact was produced.
func (b *blockBuilder) addArtifact(a artifactResponse) {
	b.blocks = append(b.blocks, contentBlock{Type: "artifact", Artifact: &a})
}

// setToolResult stamps a tool call's result onto its matching tool event,
// searching across every trace block. Mirrors activityTraceWithToolResult: a
// "tool failed" output marks the event failed, otherwise done.
func (b *blockBuilder) setToolResult(id, output string) {
	for bi := range b.blocks {
		if b.blocks[bi].Type != "trace" {
			continue
		}
		events := b.blocks[bi].Events
		for i := range events {
			if events[i].Type != "tool" || events[i].ID != id {
				continue
			}
			events[i].Status = "done"
			if strings.HasPrefix(output, toolFailedPrefix) {
				events[i].Status = "failed"
			}
			events[i].RawOutput = output
			return
		}
	}
}

// flatTrace concatenates every trace block's events in order. It reproduces the
// legacy flat activity_trace exactly, so reasoning-id numbering stays identical
// whether counted over the flat trace or the blocks.
func (b *blockBuilder) flatTrace() []activityTraceEvent {
	var out []activityTraceEvent
	for _, block := range b.blocks {
		if block.Type != "trace" {
			continue
		}
		out = append(out, block.Events...)
	}
	return out
}

// nextReasoningID is the id addResult will assign to the next reasoning event,
// computed over the flat trace so it matches the legacy numbering.
func (b *blockBuilder) nextReasoningID() string {
	return nextReasoningID(b.flatTrace())
}

// addResult appends a turn's result to the timeline at block granularity,
// reproducing appendTraceAndSpawnTitle's ordering: reasoning event (if any) →
// prose → tool-call events. The reasoning and tool events merge into the
// trailing trace block; the prose breaks the run as its own text block.
// titles.spawn is always called (matching appendTraceAndSpawnTitle).
func (b *blockBuilder) addResult(titles *reasoningTitleTracker, result llm.StreamResult) {
	reasoningID := b.nextReasoningID()
	if strings.TrimSpace(result.ReasoningContent) != "" {
		b.addTraceEvent(reasoningEvent(reasoningID, result.ReasoningContent))
	}
	b.addText(result.Content)
	for _, call := range result.ToolCalls {
		b.addTraceEvent(toolCallEvent(call))
	}
	titles.spawn(reasoningID, result.ReasoningContent)
}

// addTraceOnlyResult appends a turn's reasoning and tool-call events WITHOUT its
// prose. Used for the image prompt-compiler turn, whose content is deliberately
// hidden from the user (streamAssistantTurnSuppressingContent) and must not
// surface as a visible text block.
func (b *blockBuilder) addTraceOnlyResult(titles *reasoningTitleTracker, result llm.StreamResult) {
	reasoningID := b.nextReasoningID()
	if strings.TrimSpace(result.ReasoningContent) != "" {
		b.addTraceEvent(reasoningEvent(reasoningID, result.ReasoningContent))
	}
	for _, call := range result.ToolCalls {
		b.addTraceEvent(toolCallEvent(call))
	}
	titles.spawn(reasoningID, result.ReasoningContent)
}
