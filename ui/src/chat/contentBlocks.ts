import {
  appendReasoningDelta,
  applyReasoningTitle,
  completeTrace,
  normalizeActivityTrace,
  upsertTraceToolCall,
  upsertTraceToolResult,
  type ActivityTraceEvent,
} from "../activityTrace";
import type {
  ContentBlock,
  Message,
  ToolCallEvent,
  ToolResultEvent,
} from "../api";

// blocksFromLegacyMessage synthesizes an ordered ContentBlock[] for a persisted
// message that predates the contentBlocks wire field. It reproduces TODAY's
// fixed on-screen layout exactly so reloaded legacy threads look unchanged: the
// activity trace panel renders ABOVE the prose (see the former ThreadPanel
// composition), then the text, then one card per artifact. The reasoning-only
// fallback (a message with reasoningContent but no activityTrace) is preserved as
// a single done reasoning event, matching the prior ThreadPanel behaviour.
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
  if (
    message.reasoningContent !== undefined &&
    message.reasoningContent !== ""
  ) {
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

// Block lists that have been through normalizeContentBlocks. Normalizing parses
// tool arguments and output and builds new objects, so it must run once per
// message, not once per read: a renderer handed fresh blocks re-renders them.
const normalizedBlockLists = new WeakSet<ContentBlock[]>();

// The backend persists tool events raw (name/rawArguments only, no summary), so
// normalise each persisted trace block — exactly as the legacy path does — to
// compute the summary/preview the renderer reads via event.summary.kind.
function normalizeContentBlocks(blocks: ContentBlock[]): ContentBlock[] {
  if (normalizedBlockLists.has(blocks)) return blocks;
  const normalized = blocks.map((block): ContentBlock =>
    block.type === "trace"
      ? {
          type: "trace",
          events: normalizeActivityTrace(block.events) ?? block.events,
        }
      : block,
  );
  normalizedBlockLists.add(normalized);
  return normalized;
}

// withNormalizedBlocks is called where a message enters state (thread load, a
// settled turn, the share page), so that every later messageBlocks read returns
// the same blocks.
export function withNormalizedBlocks<T extends Message>(message: T): T {
  if (message.contentBlocks === undefined || message.contentBlocks.length === 0)
    return message;
  const contentBlocks = normalizeContentBlocks(message.contentBlocks);
  return contentBlocks === message.contentBlocks
    ? message
    : { ...message, contentBlocks };
}

// messageBlocks is the single source the renderer reads: the backend-persisted
// ordered blocks when present, otherwise lazily synthesized legacy blocks. A
// message that skipped withNormalizedBlocks is still normalized here, per call.
export function messageBlocks(message: Message): ContentBlock[] {
  if (message.contentBlocks !== undefined && message.contentBlocks.length > 0) {
    return normalizeContentBlocks(message.contentBlocks);
  }
  return blocksFromLegacyMessage(message);
}

// The streaming reducer rebuilds the same ordered block list the backend persists
// from the live SSE event stream. Each helper extends the trailing block of the
// matching kind, or pushes a fresh block when the trailing block is a different
// kind — so a text→tool→text→artifact sequence yields [text][trace][text][artifact].

// appendTextDelta extends the trailing text block, or starts a new one when the
// trailing block is not text.
export function appendTextDelta(
  blocks: ContentBlock[],
  delta: string,
): ContentBlock[] {
  if (delta === "") return blocks;
  const last = blocks[blocks.length - 1];
  if (last?.type === "text") {
    return [
      ...blocks.slice(0, -1),
      { type: "text", content: last.content + delta },
    ];
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
    return [
      ...blocks.slice(0, -1),
      { type: "trace", events: update(last.events) },
    ];
  }
  return [...blocks, { type: "trace", events: update([]) }];
}

export function appendReasoningDeltaBlock(
  blocks: ContentBlock[],
  delta: string,
): ContentBlock[] {
  return updateTraceBlock(blocks, (events) =>
    appendReasoningDelta(events, delta),
  );
}

export function upsertToolCallBlock(
  blocks: ContentBlock[],
  event: ToolCallEvent,
): ContentBlock[] {
  return updateTraceBlock(blocks, (events) =>
    upsertTraceToolCall(events, event),
  );
}

// A background reasoning title and a late tool result arrive after the answer
// prose has begun, so the trailing block is no longer the trace block they
// belong to. Target the event by id across EVERY trace block instead: the
// matching block updates, the rest pass through unchanged (the underlying
// helpers no-op when the id is absent).
export function applyReasoningTitleBlock(
  blocks: ContentBlock[],
  id: string,
  title: string,
): ContentBlock[] {
  return blocks.map((block) =>
    block.type === "trace"
      ? { type: "trace", events: applyReasoningTitle(block.events, id, title) }
      : block,
  );
}

export function upsertToolResultBlock(
  blocks: ContentBlock[],
  event: ToolResultEvent,
): ContentBlock[] {
  return blocks.map((block) =>
    block.type === "trace"
      ? { type: "trace", events: upsertTraceToolResult(block.events, event) }
      : block,
  );
}

export function appendArtifactBlock(
  blocks: ContentBlock[],
  artifact: Extract<ContentBlock, { type: "artifact" }>["artifact"],
): ContentBlock[] {
  return [
    ...blocks.filter(
      (block) =>
        !(block.type === "artifact" && block.artifact.id === artifact.id),
    ),
    { type: "artifact", artifact },
  ];
}

// completeBlocks settles every trace block's running events to done, used when a
// turn finishes streaming and the committed message carries no backend
// contentBlocks (so the just-streamed order is grafted onto the message).
export function completeBlocks(blocks: ContentBlock[]): ContentBlock[] {
  return blocks.map((block) =>
    block.type === "trace"
      ? { type: "trace", events: completeTrace(block.events) }
      : block,
  );
}

// graftStreamedBlocks reconciles a just-settled assistant message with the blocks
// reconstructed live from the stream. When the backend already sent ordered
// contentBlocks they win, normalized. Otherwise the streamed blocks (settled to
// done) become the message's contentBlocks, preserving chronological order — and
// when the answer text arrived only on the assistant_message itself (not as
// streamed deltas, so the streamed blocks carry no prose) the message content is
// appended as a trailing text block so it still renders.
export function graftStreamedBlocks(
  message: Message,
  streamedBlocks: ContentBlock[],
): Message {
  if (message.contentBlocks !== undefined && message.contentBlocks.length > 0)
    return withNormalizedBlocks(message);
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
  return withNormalizedBlocks({ ...message, contentBlocks });
}
