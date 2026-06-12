import { useCallback, useRef, useState, type RefObject } from "react";

import type { ActivityTraceEvent } from "../activityTrace";

export type ActivityTraceController = {
  /** Live trace driving the activity panel. */
  trace: ActivityTraceEvent[];
  /** Same value as `trace`, readable synchronously inside streaming closures. */
  traceRef: RefObject<ActivityTraceEvent[]>;
  /** Bridges the gap between a model-yielded tool call and its running event. */
  toolPending: boolean;
  setToolPending: (pending: boolean) => void;
  /** Apply an immutable updater to the trace, keeping state and ref in sync. */
  update: (updater: (current: ActivityTraceEvent[]) => ActivityTraceEvent[]) => void;
  /** Reset the trace for a fresh turn (also clears the pending-tool bridge). */
  clear: () => void;
};

/**
 * Owns the active activity-trace concern that ChatShell previously held inline:
 * the trace itself and the pending-tool bridge. State and its mirror ref are
 * mutated together so streaming closures (which read the ref) never diverge from
 * rendered state.
 */
export function useActivityTrace(): ActivityTraceController {
  const [trace, setTrace] = useState<ActivityTraceEvent[]>([]);
  const [toolPending, setToolPending] = useState(false);
  const traceRef = useRef<ActivityTraceEvent[]>([]);

  const update = useCallback((updater: (current: ActivityTraceEvent[]) => ActivityTraceEvent[]) => {
    const next = updater(traceRef.current);
    traceRef.current = next;
    setTrace(next);
  }, []);

  const clear = useCallback(() => {
    traceRef.current = [];
    setTrace([]);
    setToolPending(false);
  }, []);

  return {
    trace,
    traceRef,
    toolPending,
    setToolPending,
    update,
    clear,
  };
}
