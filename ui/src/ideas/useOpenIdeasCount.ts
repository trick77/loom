import { useEffect, useSyncExternalStore } from "react";

import { countIdeas, IDEAS_CHANGED, type IdeaCounts } from "../api";

// The counts are one store for every reader (the sidebar, the Ideas page
// tabs), so an IDEAS_CHANGED costs one request however many show them, and
// readers mounting together share one. Dropped when the last reader goes
// (`generation` turns away answers still on the way), so nothing outlives the
// screen that showed it.
let counts: IdeaCounts | null = null;
let generation = 0;
let mountLoad: Promise<void> | null = null;
const readers = new Set<() => void>();

// A failed load keeps the last counts; nothing shows an error for them.
function load(): Promise<void> {
  const asked = generation;
  return countIdeas().then(
    (next) => {
      if (asked !== generation) return;
      counts = next;
      readers.forEach((notify) => notify());
    },
    () => {},
  );
}

function subscribe(notify: () => void) {
  if (readers.size === 0) window.addEventListener(IDEAS_CHANGED, load);
  readers.add(notify);
  return () => {
    readers.delete(notify);
    if (readers.size > 0) return;
    window.removeEventListener(IDEAS_CHANGED, load);
    generation += 1;
    counts = null;
    mountLoad = null;
  };
}

/**
 * The open and done counts: loaded when a reader mounts and again whenever an
 * idea changes anywhere (IDEAS_CHANGED). Null until the first answer.
 */
export function useIdeaCounts(): IdeaCounts | null {
  const current = useSyncExternalStore(subscribe, () => counts);
  useEffect(() => {
    if (mountLoad !== null) return;
    const pending = load().finally(() => {
      if (mountLoad === pending) mountLoad = null;
    });
    mountLoad = pending;
  }, []);
  return current;
}

/** The sidebar's number: open ideas. */
export function useOpenIdeasCount(): number {
  return useIdeaCounts()?.open ?? 0;
}
