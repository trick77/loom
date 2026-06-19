package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/trick77/lume/internal/llm"
)

// TestBlockBuilderInterleavesChronologically drives a full round:
// reasoning → prose → tool-call → tool-result → artifact → prose, and asserts
// the ordered blocks: one merged trace block (reasoning), then text, then a
// trace block (tool, with its result stamped in place), then artifact, then text.
func TestBlockBuilderInterleavesChronologically(t *testing.T) {
	b := &blockBuilder{}

	// Round 1: reasoning + prose + a tool call.
	b.addResult(nil, llm.StreamResult{
		ReasoningContent: "let me think",
		Content:          "First, some prose.",
		ToolCalls: []llm.ToolCall{
			{ID: "call_1", Function: llm.ToolCallFunction{Name: "web_fetch", Arguments: `{"url":"x"}`}},
		},
	})
	// The tool returns and produces an artifact.
	b.setToolResult("call_1", "fetched ok")
	b.addArtifact(artifactResponse{ID: "art_1", DisplayFilename: "report.pdf"})
	// Final round: just prose, no reasoning/tools.
	b.addResult(nil, llm.StreamResult{Content: "Here is the final answer."})

	blocks := b.blocks
	if len(blocks) != 5 {
		t.Fatalf("got %d blocks, want 5: %+v", len(blocks), blocks)
	}

	// Block 0: trace with the reasoning event only (prose broke the run before tools).
	if blocks[0].Type != "trace" || len(blocks[0].Events) != 1 {
		t.Fatalf("block0 = %+v, want trace with 1 reasoning event", blocks[0])
	}
	if blocks[0].Events[0].Type != "reasoning" || blocks[0].Events[0].ID != "reasoning-1" {
		t.Fatalf("block0 event = %+v, want reasoning-1", blocks[0].Events[0])
	}
	if blocks[0].Events[0].Content != "let me think" {
		t.Fatalf("block0 reasoning content = %q", blocks[0].Events[0].Content)
	}

	// Block 1: the first prose, untrimmed/original.
	if blocks[1].Type != "text" || blocks[1].Content != "First, some prose." {
		t.Fatalf("block1 = %+v, want text 'First, some prose.'", blocks[1])
	}

	// Block 2: a NEW trace block (prose broke the run) carrying the tool event,
	// with its result stamped in place.
	if blocks[2].Type != "trace" || len(blocks[2].Events) != 1 {
		t.Fatalf("block2 = %+v, want trace with 1 tool event", blocks[2])
	}
	tool := blocks[2].Events[0]
	if tool.Type != "tool" || tool.ID != "call_1" || tool.Name != "web_fetch" {
		t.Fatalf("block2 tool event = %+v", tool)
	}
	if tool.Status != "done" || tool.RawOutput != "fetched ok" {
		t.Fatalf("tool result not stamped: status=%q output=%q", tool.Status, tool.RawOutput)
	}

	// Block 3: the artifact, produced at this position.
	if blocks[3].Type != "artifact" || blocks[3].Artifact == nil || blocks[3].Artifact.ID != "art_1" {
		t.Fatalf("block3 = %+v, want artifact art_1", blocks[3])
	}

	// Block 4: the final prose.
	if blocks[4].Type != "text" || blocks[4].Content != "Here is the final answer." {
		t.Fatalf("block4 = %+v, want final text", blocks[4])
	}

	// flatTrace concatenates trace events in order, matching the legacy trace.
	flat := b.flatTrace()
	if len(flat) != 2 || flat[0].ID != "reasoning-1" || flat[1].ID != "call_1" {
		t.Fatalf("flatTrace = %+v, want [reasoning-1, call_1]", flat)
	}

	// The blocks serialize to the agreed wire shape.
	if _, err := json.Marshal(blocks); err != nil {
		t.Fatalf("marshal blocks: %v", err)
	}
}

// TestBlockBuilderMergesConsecutiveTraceEvents verifies that a reasoning event
// and the round's tool calls land in ONE trace block when no prose intervenes —
// the merge that produces a single collapsible panel.
func TestBlockBuilderMergesConsecutiveTraceEvents(t *testing.T) {
	b := &blockBuilder{}
	b.addResult(nil, llm.StreamResult{
		ReasoningContent: "thinking",
		// No prose this round, so reasoning and the tool call stay in one trace block.
		ToolCalls: []llm.ToolCall{
			{ID: "call_a", Function: llm.ToolCallFunction{Name: "search"}},
			{ID: "call_b", Function: llm.ToolCallFunction{Name: "fetch"}},
		},
	})
	if len(b.blocks) != 1 || b.blocks[0].Type != "trace" {
		t.Fatalf("want one trace block, got %+v", b.blocks)
	}
	if len(b.blocks[0].Events) != 3 {
		t.Fatalf("want 3 merged events, got %d", len(b.blocks[0].Events))
	}
}

// TestBlockBuilderSkipsBlankText ensures whitespace-only prose never creates an
// empty text block that would split an otherwise-mergeable trace run.
func TestBlockBuilderSkipsBlankText(t *testing.T) {
	b := &blockBuilder{}
	b.addText("   \n\t ")
	if len(b.blocks) != 0 {
		t.Fatalf("blank text should be skipped, got %+v", b.blocks)
	}
}

// TestBlockBuilderTraceOnlyResultOmitsProse verifies the suppressed-content path
// (the image prompt compiler) records reasoning/tool events but NOT the hidden
// prose, so it never leaks into the timeline.
func TestBlockBuilderTraceOnlyResultOmitsProse(t *testing.T) {
	b := &blockBuilder{}
	b.addTraceOnlyResult(nil, llm.StreamResult{
		ReasoningContent: "internal reasoning",
		Content:          "HIDDEN compiler prose",
		ToolCalls: []llm.ToolCall{
			{ID: "call_img", Function: llm.ToolCallFunction{Name: "generate_image"}},
		},
	})
	for _, block := range b.blocks {
		if block.Type == "text" {
			t.Fatalf("suppressed prose leaked as a text block: %+v", block)
		}
	}
	if len(b.blocks) != 1 || b.blocks[0].Type != "trace" || len(b.blocks[0].Events) != 2 {
		t.Fatalf("want one trace block with reasoning+tool, got %+v", b.blocks)
	}
}

// TestMergeIntoBlocksStampsReasoningTitles verifies titles generated for the flat
// trace also land on the separate reasoning events inside the content blocks,
// since they share the same reasoning ids.
func TestMergeIntoBlocksStampsReasoningTitles(t *testing.T) {
	b := &blockBuilder{}
	b.addResult(nil, llm.StreamResult{ReasoningContent: "deliberating", Content: "answer"})
	if b.blocks[0].Events[0].ID != "reasoning-1" {
		t.Fatalf("setup: want reasoning-1, got %q", b.blocks[0].Events[0].ID)
	}

	tracker := &reasoningTitleTracker{titles: map[string]string{"reasoning-1": "Deliberation"}}
	tracker.mergeIntoBlocks(b.blocks)

	if got := b.blocks[0].Events[0].Title; got != "Deliberation" {
		t.Fatalf("block reasoning title = %q, want Deliberation", got)
	}
}

// TestBlockBuilderSetToolResultFailure verifies a "tool failed" output marks the
// matching event failed.
func TestBlockBuilderSetToolResultFailure(t *testing.T) {
	b := &blockBuilder{}
	b.addResult(nil, llm.StreamResult{
		ToolCalls: []llm.ToolCall{{ID: "call_x", Function: llm.ToolCallFunction{Name: "t"}}},
	})
	b.setToolResult("call_x", toolFailedPrefix+": boom")
	if b.blocks[0].Events[0].Status != "failed" {
		t.Fatalf("want failed status, got %q", b.blocks[0].Events[0].Status)
	}
}
