/**
 * React binding for the per-thread run registry in streamRuns.ts.
 *
 * What lives here that the pure reducers cannot hold: the AbortController per run
 * key, and the counter behind the provisional keys. The runs record itself is
 * plain state — every consumer reads it from render scope, so unlike the old
 * single-slot stream state there is no ref to keep in sync with it.
 */
import { useCallback, useRef, useState } from "react";

import {
  beginRun,
  EMPTY_RUN,
  endRun,
  patchRun,
  rekeyRun,
  type RunKey,
  type RunState,
  type StreamRuns,
} from "./streamRuns";

export function useStreamRuns() {
  const [runs, setRuns] = useState<StreamRuns>({});
  const abortsRef = useRef<Map<RunKey, AbortController>>(new Map());
  // The client's id for the send behind each run, named by a stop.
  const sendIdsRef = useRef<Map<RunKey, string>>(new Map());
  // Each start-screen send takes its own provisional key: createThread (plus a
  // possible image-upload flush) can take seconds, and a second send in that
  // window must not land on the first one's key.
  const provisionalCounterRef = useRef(0);

  const commit = useCallback((next: (current: StreamRuns) => StreamRuns) => {
    setRuns(next);
  }, []);

  const begin = useCallback(
    (key: RunKey, controller: AbortController, sendId?: string) => {
      abortsRef.current.set(key, controller);
      if (sendId === undefined) sendIdsRef.current.delete(key);
      else sendIdsRef.current.set(key, sendId);
      commit((current) => beginRun(current, key));
    },
    [commit],
  );

  // `next` may be a function so a caller can derive the patch from the run's
  // current state — needed where a patch merges rather than replaces (the two
  // source snapshots each carry only their own kind; see mergeSourceSnapshot).
  const patch = useCallback(
    (
      key: RunKey,
      next: Partial<RunState> | ((run: RunState) => Partial<RunState>),
    ) => {
      commit((current) =>
        patchRun(
          current,
          key,
          typeof next === "function" ? next(current[key] ?? EMPTY_RUN) : next,
        ),
      );
    },
    [commit],
  );

  const rekey = useCallback(
    (from: RunKey, to: RunKey) => {
      const controller = abortsRef.current.get(from);
      if (controller !== undefined) {
        abortsRef.current.delete(from);
        abortsRef.current.set(to, controller);
      }
      const sendId = sendIdsRef.current.get(from);
      sendIdsRef.current.delete(from);
      if (sendId !== undefined) sendIdsRef.current.set(to, sendId);
      commit((current) => rekeyRun(current, from, to));
    },
    [commit],
  );

  const end = useCallback(
    (
      key: RunKey,
      options: {
        keepFailedTurnVisible: boolean;
        controller?: AbortController | null;
      },
    ) => {
      // Only end the run if it is still ours. A superseded run's `finally` can land
      // after the replacement has already registered under the same key: dropping
      // the controller then would leave the live run unstoppable, and ending its
      // state would make it invisible for the rest of the turn (patchRun drops
      // every later patch for a key with no run).
      const controller = options.controller ?? null;
      const stored = abortsRef.current.get(key);
      if (controller !== null && stored !== undefined && stored !== controller)
        return;
      if (stored !== undefined) {
        abortsRef.current.delete(key);
        sendIdsRef.current.delete(key);
      }
      commit((current) =>
        endRun(current, key, {
          keepFailedTurnVisible: options.keepFailedTurnVisible,
        }),
      );
    },
    [commit],
  );

  // has reports whether a run is in flight on key, read from the ref so a
  // caller outside render (a load callback) sees the current answer.
  const has = useCallback((key: RunKey) => abortsRef.current.has(key), []);

  const abort = useCallback((key: RunKey) => {
    abortsRef.current.get(key)?.abort();
  }, []);

  // A user-requested stop is posted to the server before the fetch is aborted,
  // and the server may close the stream first; the run's catch asks whether a
  // stop was requested so that early close is not reported as a dropped
  // connection.
  const stopRequestedRef = useRef(new WeakSet<AbortController>());
  const markStopRequested = useCallback((key: RunKey) => {
    const controller = abortsRef.current.get(key);
    if (controller !== undefined) stopRequestedRef.current.add(controller);
    return controller;
  }, []);
  const stopRequested = useCallback(
    (controller: AbortController) => stopRequestedRef.current.has(controller),
    [],
  );
  // A stop that never reached the server stopped nothing: the run goes on.
  const unmarkStopRequested = useCallback((controller: AbortController) => {
    stopRequestedRef.current.delete(controller);
  }, []);
  const sendIdOf = useCallback(
    (key: RunKey) => sendIdsRef.current.get(key),
    [],
  );

  const abortAll = useCallback(() => {
    abortsRef.current.forEach((controller) => controller.abort());
    abortsRef.current.clear();
  }, []);

  const nextProvisionalKey = useCallback(() => {
    provisionalCounterRef.current += 1;
    return `new:${provisionalCounterRef.current}`;
  }, []);

  return {
    runs,
    begin,
    patch,
    rekey,
    end,
    has,
    abort,
    abortAll,
    markStopRequested,
    unmarkStopRequested,
    stopRequested,
    sendIdOf,
    nextProvisionalKey,
  };
}
