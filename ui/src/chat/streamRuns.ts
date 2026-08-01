/**
 * Per-thread streaming runs.
 *
 * A "run" is one assistant turn in flight. Loom used to keep exactly one run's
 * worth of state (blocks, sources, tool-pending, abort controller) in ThreadShell,
 * which is why sending anywhere was blocked while any thread streamed. The backend
 * never needed that: activeStreamRegistry is keyed by {userID, threadID} and
 * preempts rather than serializes, so runs on different threads have always been
 * free to overlap.
 *
 * Runs are keyed, and the key space is deliberately wider than "thread id":
 *
 *   thread:<id>   a normal turn on a persisted thread
 *   new:<n>       a turn started from the start screen, before createThread has
 *                 returned. Each send mints its own counter value, so two quick
 *                 sends from /new cannot collide during the (potentially
 *                 multi-second, with image uploads) window before rekeyRun.
 *   incognito     the ephemeral standalone chat
 *
 * An absent key means idle. `status: "failed"` is a deliberate resting state, not
 * a transient: a failed turn keeps its partial blocks and its error on screen
 * until the next send on that key clears them.
 */
import type { Citation, ContentBlock } from "../api";

export type RunKey = string;

export type RunState = {
  blocks: ContentBlock[];
  sources: Citation[];
  toolPending: boolean;
  status: "idle" | "streaming" | "failed";
  error: string;
};

export type StreamRuns = Record<RunKey, RunState>;

/** The state a surface renders when its key has no run. Never mutate. */
export const EMPTY_RUN: RunState = Object.freeze({
  blocks: Object.freeze([]) as unknown as ContentBlock[],
  sources: Object.freeze([]) as unknown as Citation[],
  toolPending: false,
  status: "idle",
  error: "",
}) as RunState;

export function threadRunKey(threadID: string): RunKey {
  return `thread:${threadID}`;
}

export const INCOGNITO_RUN_KEY: RunKey = "incognito";

export function provisionalRunKey(counter: number): RunKey {
  return `new:${counter}`;
}

export function selectRun(runs: StreamRuns, key: RunKey | null): RunState {
  if (key === null) return EMPTY_RUN;
  return runs[key] ?? EMPTY_RUN;
}

export function isStreaming(runs: StreamRuns, key: RunKey | null): boolean {
  if (key === null) return false;
  return runs[key]?.status === "streaming";
}

/** True while any run is in flight, regardless of which thread owns it. */
export function anyStreaming(runs: StreamRuns): boolean {
  return Object.values(runs).some((run) => run.status === "streaming");
}

/** Start a turn on `key`, discarding whatever a previous failed turn left there. */
export function beginRun(runs: StreamRuns, key: RunKey): StreamRuns {
  return {
    ...runs,
    [key]: {
      blocks: [],
      sources: [],
      toolPending: false,
      status: "streaming",
      error: "",
    },
  };
}

/**
 * Patch a live run. A patch for a key that has already ended is dropped rather
 * than resurrecting the run — late SSE callbacks landing after `endRun` must not
 * put a finished turn back on screen.
 */
export function patchRun(
  runs: StreamRuns,
  key: RunKey,
  patch: Partial<RunState>,
): StreamRuns {
  const current = runs[key];
  if (current === undefined) return runs;
  return { ...runs, [key]: { ...current, ...patch } };
}

/**
 * End a turn. A failed turn is kept (status "failed") so its partial blocks and
 * error stay visible on the thread that owns them; a clean turn's key is removed
 * entirely, which is what makes "absent = idle" hold.
 */
export function endRun(
  runs: StreamRuns,
  key: RunKey,
  options: { keepFailedTurnVisible: boolean },
): StreamRuns {
  const current = runs[key];
  if (current === undefined) return runs;
  if (options.keepFailedTurnVisible) {
    return { ...runs, [key]: { ...current, status: "failed" } };
  }
  const next = { ...runs };
  delete next[key];
  return next;
}

/**
 * Move a run to a new key, used once when a start-screen send learns its thread
 * id. Anything already sitting on `to` is replaced — the thread was just created,
 * so it cannot legitimately own a run yet.
 */
export function rekeyRun(
  runs: StreamRuns,
  from: RunKey,
  to: RunKey,
): StreamRuns {
  const current = runs[from];
  if (current === undefined || from === to) return runs;
  const next = { ...runs };
  delete next[from];
  next[to] = current;
  return next;
}
