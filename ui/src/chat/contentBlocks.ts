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

// trailsAnswer: a tool's file (a run_python chart, a document), not a picture
// the image model made (it names its model): a generated image stays where it
// arrived, its caption written around it.
export function trailsAnswer(block: ContentBlock): boolean {
  return block.type === "artifact" && !block.artifact.model;
}

// ArrangedBlock is a block as rendered: key survives the stream (a text block
// split by a placed chart keeps its first part's key), index is the block's
// position in the source list (the live trace panel is found by it).
export interface ArrangedBlock {
  key: string;
  index: number;
  block: ContentBlock;
}

// A placement line: a markdown image naming a stored file bare (the backend's
// run_python result hands the model exactly this, `![caption](name)`). The
// caption may hold one level of brackets ("[bpm]"), the destination CommonMark's
// angle-bracket form and a title.
const REF_SOURCE =
  /!\[(?:[^[\]\n]|\[[^[\]\n]*\])*\]\(\s*(?:<([^>\n]+)>|([^)\s]+))(?:\s+"[^"\n]*")?\s*\)/
    .source;
const ARTIFACT_REF = new RegExp(REF_SOURCE, "g");
const REF_AT_START = new RegExp(`^${REF_SOURCE}`);
// A bare file name: no scheme, no path. A URL or /api/… path is a real image.
// out/ is where the model's own code saved it: the same file.
const BARE_NAME = /^[^\s/\\:?#]+\.[A-Za-z0-9]+$/;
const OUT_DIR = /^(?:\.\/)?out\//;
const FENCE = /^ {0,3}(```|~~~)/;
// Lines markdown reads as code or a table cell: a placement there stays text.
const VERBATIM_LINE = /^(?: {4}|\t)|^\s*\|/;
// What may stand before a placement line's image and leave with it: a list
// item's or a quote's marker; the chart breaks the list or quote anyway.
const LINE_MARKUP = /^\s*(?:(?:[-*+]|\d+[.)])\s+|>\s*)*$/;

// arrangeBlocks is the order a message renders in. A tool's file
// (trailsAnswer: a run_python chart, a document) shows where the answer's
// placement line names it; one left unplaced goes below everything else, in
// arrival order. The user first asked for "charts are after" (drawn before
// the answer, they led it: the tool loop ends at a text-only round, so
// "answer first, draw last" lost them), then for each "in the answer where it
// refers to them". A line naming no file of this message (an earlier answer's
// chart, a renamed one) and a second line for a placed chart are dropped:
// MarkdownImage shows nothing for a bare name anyway. Streaming, a placement
// line still being written at the end is held back, never shown raw.
export function arrangeBlocks(
  blocks: ContentBlock[],
  streaming = false,
): ArrangedBlock[] {
  const byName = new Map<string, Extract<ContentBlock, { type: "artifact" }>>();
  for (const block of blocks) {
    if (trailsAnswer(block) && block.type === "artifact") {
      const name = block.artifact.displayFilename;
      if (!byName.has(name)) byName.set(name, block);
    }
  }
  const placed = new Set<string>();
  const out: ArrangedBlock[] = [];
  blocks.forEach((block, index) => {
    if (block.type === "artifact") {
      if (!trailsAnswer(block))
        out.push({ key: `a:${block.artifact.id}`, index, block });
      return;
    }
    if (block.type !== "text") {
      out.push({ key: `${index}`, index, block });
      return;
    }
    // The growing last block is held back and tokenized afresh per token;
    // every other text block is tokenized once (refLines caches it).
    const lines =
      streaming && index === blocks.length - 1
        ? tokenizeRefs(holdBackPartialRef(block.content))
        : refLines(block);
    let part = 0;
    const pushText = (lines: string[]) => {
      const text = lines.join("\n").replace(/^\n+|\n+$/g, "");
      if (text.trim() === "") return;
      out.push({
        key: part === 0 ? `${index}` : `${index}.${part}`,
        index,
        block: { type: "text", content: text },
      });
      part += 1;
    };
    splitAtRefs(lines, (name) => {
      const ref = byName.get(name);
      if (ref === undefined || placed.has(ref.artifact.id)) return undefined;
      placed.add(ref.artifact.id);
      return ref;
    }).forEach((piece) => {
      if (Array.isArray(piece)) {
        pushText(piece);
        return;
      }
      out.push({ key: `a:${piece.artifact.id}`, index, block: piece });
    });
  });
  blocks.forEach((block, index) => {
    if (
      block.type === "artifact" &&
      trailsAnswer(block) &&
      !placed.has(block.artifact.id)
    )
      out.push({ key: `a:${block.artifact.id}`, index, block });
  });
  return out;
}

// RefLine is one source line: verbatim, or its text and placement refs in
// order (refOnly: nothing else on it but list or quote markup).
type RefLine =
  | { raw: string }
  | { parts: Array<string | { name: string }>; refOnly: boolean };

// A text block is immutable once a later one exists, and a settled message's
// blocks are normalized once: its tokens are computed once, not per token
// of the stream that follows.
const refLineCache = new WeakMap<ContentBlock, RefLine[]>();

function refLines(block: Extract<ContentBlock, { type: "text" }>): RefLine[] {
  let lines = refLineCache.get(block);
  if (lines === undefined) {
    lines = tokenizeRefs(block.content);
    refLineCache.set(block, lines);
  }
  return lines;
}

// tokenizeRefs finds the placement lines' images outside code (fences,
// indented blocks, inline spans) and tables.
function tokenizeRefs(content: string): RefLine[] {
  const lines: RefLine[] = [];
  let inFence = false;
  for (const line of content.split("\n")) {
    const fence = FENCE.test(line);
    if (fence) inFence = !inFence;
    if (fence || inFence || VERBATIM_LINE.test(line)) {
      lines.push({ raw: line });
      continue;
    }
    const code = codeSpans(line);
    const parts: Array<string | { name: string }> = [];
    let from = 0;
    for (const match of line.matchAll(ARTIFACT_REF)) {
      const at = match.index;
      if (code.some(([start, end]) => at >= start && at < end)) continue;
      const name = (match[1] ?? match[2]).replace(OUT_DIR, "");
      if (!BARE_NAME.test(name)) continue;
      parts.push(line.slice(from, at), { name });
      from = at + match[0].length;
    }
    if (parts.length === 0) {
      lines.push({ raw: line });
      continue;
    }
    if (LINE_MARKUP.test(parts[0] as string)) parts[0] = "";
    parts.push(line.slice(from));
    const refOnly = parts.every(
      (part) => typeof part !== "string" || part.trim() === "",
    );
    lines.push({ parts, refOnly });
  }
  return lines;
}

// codeSpans is where a line's inline code runs: a backtick run up to the
// next run of the same length.
function codeSpans(line: string): Array<[number, number]> {
  const runs = [...line.matchAll(/`+/g)];
  const spans: Array<[number, number]> = [];
  for (let i = 0; i < runs.length; i += 1) {
    const close = runs.findIndex(
      (run, j) => j > i && run[0].length === runs[i][0].length,
    );
    if (close === -1) continue;
    spans.push([runs[i].index, runs[close].index + runs[close][0].length]);
    i = close;
  }
  return spans;
}

// splitAtRefs cuts the lines at their placement refs: text pieces as lines,
// and the artifact block take() hands back for a name. A ref take() refuses
// is dropped; a line left empty goes with one blank line around it, so the
// paragraphs around it keep a single break.
function splitAtRefs<T>(
  lines: RefLine[],
  take: (name: string) => T | undefined,
): Array<string[] | T> {
  const pieces: Array<string[] | T> = [];
  let current: string[] = [];
  let skipBlank = false;
  for (const line of lines) {
    if ("raw" in line) {
      if (!(skipBlank && line.raw.trim() === "")) current.push(line.raw);
      skipBlank = false;
      continue;
    }
    skipBlank = false;
    let kept = "";
    for (const part of line.parts) {
      if (typeof part === "string") {
        kept += part;
        continue;
      }
      const artifact = take(part.name);
      if (artifact === undefined) continue;
      if (kept.trim() !== "") current.push(kept);
      pieces.push(current, artifact);
      current = [];
      kept = "";
    }
    if (line.refOnly) {
      skipBlank =
        current.length === 0 || current[current.length - 1].trim() === "";
      continue;
    }
    current.push(kept);
  }
  pieces.push(current);
  return pieces;
}

// holdBackPartialRef cuts a placement line still being streamed off the end
// (outside an open code fence): from the last "![" on the last line, unless
// a whole image starts there. A token later it is a chart, not raw text.
function holdBackPartialRef(content: string): string {
  const fences = content.split("\n").filter((line) => FENCE.test(line));
  if (fences.length % 2 === 1) return content;
  const start = content.lastIndexOf("![");
  if (start === -1) return content;
  const tail = content.slice(start);
  if (tail.includes("\n") || REF_AT_START.test(tail)) return content;
  return content.slice(0, start);
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
