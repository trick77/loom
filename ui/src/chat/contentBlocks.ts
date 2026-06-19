import {
  appendReasoningDelta,
  applyReasoningTitle,
  completeTrace,
  normalizeActivityTrace,
  upsertTraceToolCall,
  upsertTraceToolResult,
  type ActivityTraceEvent,
} from "../activityTrace";
import type { ContentBlock, Message, ToolCallEvent, ToolResultEvent } from "../api";

// blocksFromLegacyMessage synthesizes an ordered ContentBlock[] for a persisted
// message that predates the contentBlocks wire field. It reproduces TODAY's
// fixed on-screen layout exactly so reloaded legacy threads look unchanged: the
// activity trace panel renders ABOVE the prose (see the former ChatPanel
// composition), then the text, then one card per artifact. The reasoning-only
// fallback (a message with reasoningContent but no activityTrace) is preserved as
// a single done reasoning event, matching the prior ChatPanel behaviour.
export function blocksFromLegacyMessage(message: Message): ContentBlock[] {
  const blocks: ContentBlock[] = [];

  const traceEvents = legacyTraceEvents(message);
  if (traceEvents !== undefined && traceEvents.length > 0) {
    blocks.push({ type: "trace", events: traceEvents });
  }

  if (message.content !== "") {
    blocks.push({ type: "text", content: message.content });
  }

  for (const artifact of message.artifacts ?? []) {
    blocks.push({ type: "artifact", artifact });
  }

  return blocks;
}

function legacyTraceEvents(message: Message): ActivityTraceEvent[] | undefined {
  const normalized = normalizeActivityTrace(message.activityTrace);
  if (normalized !== undefined && normalized.length > 0) return normalized;
  if (message.reasoningContent !== undefined && message.reasoningContent !== "") {
    return [
      {
        id: `${message.id}-reasoning`,
        type: "reasoning",
        content: message.reasoningContent,
        status: "done",
      },
    ];
  }
  return undefined;
}

// messageBlocks is the single source the renderer reads: the backend-persisted
// ordered blocks when present, otherwise lazily synthesized legacy blocks.
export function messageBlocks(message: Message): ContentBlock[] {
  if (message.contentBlocks !== undefined && message.contentBlocks.length > 0) {
    // The backend persists tool events raw (name/rawArguments only, no summary),
    // so normalise each persisted trace block — exactly as the legacy path does —
    // to compute the summary/preview the renderer reads via event.summary.kind.
    return message.contentBlocks.map((block) =>
      block.type === "trace" ? { type: "trace", events: normalizeActivityTrace(block.events) ?? block.events } : block,
    );
  }
  return blocksFromLegacyMessage(message);
}

// The streaming reducer rebuilds the same ordered block list the backend persists
// from the live SSE event stream. Each helper extends the trailing block of the
// matching kind, or pushes a fresh block when the trailing block is a different
// kind — so a text→tool→text→artifact sequence yields [text][trace][text][artifact].

// appendTextDelta extends the trailing text block, or starts a new one when the
// trailing block is not text.
export function appendTextDelta(blocks: ContentBlock[], delta: string): ContentBlock[] {
  if (delta === "") return blocks;
  const last = blocks[blocks.length - 1];
  if (last?.type === "text") {
    return [...blocks.slice(0, -1), { type: "text", content: last.content + delta }];
  }
  return [...blocks, { type: "text", content: delta }];
}

// updateTraceBlock applies an immutable updater to the trailing trace block's
// events, pushing a fresh (empty) trace block first when the trailing block is
// not a trace block.
function updateTraceBlock(
  blocks: ContentBlock[],
  update: (events: ActivityTraceEvent[]) => ActivityTraceEvent[],
): ContentBlock[] {
  const last = blocks[blocks.length - 1];
  if (last?.type === "trace") {
    return [...blocks.slice(0, -1), { type: "trace", events: update(last.events) }];
  }
  return [...blocks, { type: "trace", events: update([]) }];
}

export function appendReasoningDeltaBlock(blocks: ContentBlock[], delta: string): ContentBlock[] {
  return updateTraceBlock(blocks, (events) => appendReasoningDelta(events, delta));
}

export function upsertToolCallBlock(blocks: ContentBlock[], event: ToolCallEvent): ContentBlock[] {
  return updateTraceBlock(blocks, (events) => upsertTraceToolCall(events, event));
}

// A background reasoning title and a late tool result arrive after the answer
// prose has begun, so the trailing block is no longer the trace block they
// belong to. Target the event by id across EVERY trace block instead: the
// matching block updates, the rest pass through unchanged (the underlying
// helpers no-op when the id is absent).
export function applyReasoningTitleBlock(blocks: ContentBlock[], id: string, title: string): ContentBlock[] {
  return blocks.map((block) =>
    block.type === "trace" ? { type: "trace", events: applyReasoningTitle(block.events, id, title) } : block,
  );
}

export function upsertToolResultBlock(blocks: ContentBlock[], event: ToolResultEvent): ContentBlock[] {
  return blocks.map((block) =>
    block.type === "trace" ? { type: "trace", events: upsertTraceToolResult(block.events, event) } : block,
  );
}

export function appendArtifactBlock(
  blocks: ContentBlock[],
  artifact: Extract<ContentBlock, { type: "artifact" }>["artifact"],
): ContentBlock[] {
  return [
    ...blocks.filter((block) => !(block.type === "artifact" && block.artifact.id === artifact.id)),
    { type: "artifact", artifact },
  ];
}

// completeBlocks settles every trace block's running events to done, used when a
// turn finishes streaming and the committed message carries no backend
// contentBlocks (so the just-streamed order is grafted onto the message).
export function completeBlocks(blocks: ContentBlock[]): ContentBlock[] {
  return blocks.map((block) =>
    block.type === "trace" ? { type: "trace", events: completeTrace(block.events) } : block,
  );
}

// graftStreamedBlocks reconciles a just-settled assistant message with the blocks
// reconstructed live from the stream. When the backend already sent ordered
// contentBlocks they win untouched. Otherwise the streamed blocks (settled to
// done) become the message's contentBlocks, preserving chronological order — and
// when the answer text arrived only on the assistant_message itself (not as
// streamed deltas, so the streamed blocks carry no prose) the message content is
// appended as a trailing text block so it still renders.
export function graftStreamedBlocks(message: Message, streamedBlocks: ContentBlock[]): Message {
  if (message.contentBlocks !== undefined && message.contentBlocks.length > 0) return message;
  if (streamedBlocks.length === 0) return message;
  const completed = completeBlocks(streamedBlocks);
  // The authoritative final answer text lives on the settled message, not the
  // streamed deltas (which can be partial, or the answer may have arrived only on
  // the message). Replace the trailing text block — the final answer — with it,
  // preserving earlier intermediate prose and the chronological position of the
  // trace/artifact blocks. When the stream produced no text block, append one.
  const contentBlocks = [...completed];
  if (message.content !== "") {
    let lastTextIndex = -1;
    for (let index = contentBlocks.length - 1; index >= 0; index -= 1) {
      if (contentBlocks[index].type === "text") {
        lastTextIndex = index;
        break;
      }
    }
    if (lastTextIndex === -1) {
      contentBlocks.push({ type: "text", content: message.content });
    } else {
      contentBlocks[lastTextIndex] = { type: "text", content: message.content };
    }
  }
  return { ...message, contentBlocks };
}
