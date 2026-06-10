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
  /** Whether the user has explicitly expanded the active trace this turn. */
  userExpanded: boolean;
  /** Same value as `userExpanded`, readable synchronously in streaming closures. */
  expandedRef: RefObject<boolean>;
  /** Apply an immutable updater to the trace, keeping state and ref in sync. */
  update: (updater: (current: ActivityTraceEvent[]) => ActivityTraceEvent[]) => void;
  /** Reset the trace for a fresh turn (also clears the pending-tool bridge). */
  clear: () => void;
  /** Set the user-expanded flag, keeping state and ref in sync. */
  setUserExpanded: (expanded: boolean) => void;
  /** Collapse the trace back to its default (un-expanded) state. */
  resetExpansion: () => void;
};

/**
 * Owns the active activity-trace concern that ChatShell previously held inline:
 * the trace itself, the pending-tool bridge, and the user-expansion state. State
 * and its mirror ref are mutated together so streaming closures (which read the
 * ref) never diverge from rendered state.
 */
export function useActivityTrace(): ActivityTraceController {
  const [trace, setTrace] = useState<ActivityTraceEvent[]>([]);
  const [toolPending, setToolPending] = useState(false);
  const [userExpanded, setUserExpandedState] = useState(false);
  const traceRef = useRef<ActivityTraceEvent[]>([]);
  const expandedRef = useRef(false);

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

  const setUserExpanded = useCallback((expanded: boolean) => {
    expandedRef.current = expanded;
    setUserExpandedState(expanded);
  }, []);

  const resetExpansion = useCallback(() => setUserExpanded(false), [setUserExpanded]);

  return {
    trace,
    traceRef,
    toolPending,
    setToolPending,
    userExpanded,
    expandedRef,
    update,
    clear,
    setUserExpanded,
    resetExpansion,
  };
}
