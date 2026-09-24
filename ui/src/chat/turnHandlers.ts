import type { Citation, Message, StreamHandlers, Thread } from "../api";
import {
  appendArtifactBlock,
  appendReasoningDeltaBlock,
  appendTextDelta,
  applyReasoningTitleBlock,
  upsertToolCallBlock,
  upsertToolResultBlock,
} from "./contentBlocks";
import type { ContentBlock } from "../api/types";
import type { RunState } from "./streamRuns";

// RunPatch is what a turn writes into its run: a partial state, or a function
// of the current state for merges that depend on it.
export type RunPatch =
  Partial<RunState> | ((run: RunState) => Partial<RunState>);

// newTempID mints a client-side id for an optimistic message that the server
// has not confirmed yet.
export function newTempID(prefix: string): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

// Each sources event is a full snapshot of *its own kind* only: knowledge_sources
// carries the user's numbered documents (no url) once before the model runs,
// web_sources carries the gathered pages (url) after every tool round. Replacing
// the whole list on either would drop the other kind — since documents became
// citable, the first search result would unresolve every document [n] pill
// mid-answer and shift the display numbering that is meant to be append-only.
// So each event replaces only its own kind. Documents lead: they hold the low
// indices, numbered before the tool loop.
function isWebCitation(citation: Citation): boolean {
  return typeof citation.url === "string" && citation.url !== "";
}

export function mergeSourceSnapshot(
  previous: Citation[],
  incoming: Citation[],
  incomingAreWeb: boolean,
): Citation[] {
  const kept = previous.filter(
    (citation) => isWebCitation(citation) !== incomingAreWeb,
  );
  return incomingAreWeb ? [...kept, ...incoming] : [...incoming, ...kept];
}

// createTurnHandlers builds the stream handlers one turn needs. The ordered
// content blocks of the turn accumulate in a closure-local array, the single
// source of truth for the graft at turn end (the rendered copy lives on the
// run, but a run can be ended or superseded from elsewhere). Every event that
// only touches blocks or run state is handled here; the caller supplies what
// differs between the persisted and the incognito flows: what to do with the
// confirmed user message, the finished assistant message (given the live
// blocks to graft) and a thread update.
export function createTurnHandlers(opts: {
  patch: (next: RunPatch) => void;
  onUserMessage?: (message: Message) => void;
  onAssistantMessage: (message: Message, liveBlocks: ContentBlock[]) => void;
  onThread?: (thread: Thread) => void;
}): { handlers: StreamHandlers; liveBlocks: () => ContentBlock[] } {
  let liveBlocks: ContentBlock[] = [];
  // One network chunk dispatches many events; every one used to write the run,
  // and every run write re-rendered the whole shell. Block updates land on the
  // local array at once and reach the run once per microtask, so a chunk's
  // worth of deltas costs one render.
  let flushQueued = false;
  const flush = () => {
    if (!flushQueued) return;
    flushQueued = false;
    opts.patch({ blocks: liveBlocks });
  };
  const applyBlocks = (
    updater: (current: ContentBlock[]) => ContentBlock[],
  ) => {
    liveBlocks = updater(liveBlocks);
    if (flushQueued) return;
    flushQueued = true;
    queueMicrotask(flush);
  };
  const handlers: StreamHandlers = {
    onUserMessage: (message) => opts.onUserMessage?.(message),
    onDelta: (delta) =>
      applyBlocks((current) => appendTextDelta(current, delta)),
    onReasoningDelta: (delta) =>
      applyBlocks((current) => appendReasoningDeltaBlock(current, delta)),
    onReasoningTitle: (event) =>
      applyBlocks((current) =>
        applyReasoningTitleBlock(current, event.id, event.title),
      ),
    onToolPending: () => opts.patch({ toolPending: true }),
    onToolCall: (event) => {
      opts.patch({ toolPending: false });
      applyBlocks((current) => upsertToolCallBlock(current, event));
    },
    onToolResult: (event) =>
      applyBlocks((current) => upsertToolResultBlock(current, event)),
    onArtifact: (artifact) =>
      applyBlocks((current) => appendArtifactBlock(current, artifact)),
    onWebSources: (sources: Citation[]) =>
      opts.patch((run) => ({
        sources: mergeSourceSnapshot(run.sources, sources, true),
      })),
    onKnowledgeSources: (sources: Citation[]) =>
      opts.patch((run) => ({
        sources: mergeSourceSnapshot(run.sources, sources, false),
      })),
    onAssistantMessage: (message) => {
      // Drop a queued flush: it would put the live blocks back onto the run
      // after the reset below.
      flushQueued = false;
      opts.onAssistantMessage(message, liveBlocks);
      // The persisted message now carries the blocks; the run's live copy is
      // done with.
      opts.patch({ blocks: [], sources: [], toolPending: false });
    },
    onThread: (thread) => opts.onThread?.(thread),
  };
  return { handlers, liveBlocks: () => liveBlocks };
}
