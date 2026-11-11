import {
  type CSSProperties,
  createContext,
  type ReactNode,
  type RefObject,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";

// SEEN is the share of an element in view that counts as seen: the observer
// reports any overlap, a sliver below the fold would play unseen. A card
// taller than the viewport (a short phone, a page zoomed in) never shows
// SEEN of itself: filling SEEN of the viewport counts too, or its parts would
// wait at their start, hidden, for good.
export const SEEN = 0.6;

// The observer reports every 5 % so a tall card's growing share is heard.
const THRESHOLDS = Array.from({ length: 21 }, (_, i) => i / 20);

// REVEAL_MS outlasts the longest reveal with room to spare (the air curve:
// 120 ms + 900 ms; 48 rain hours: 47 × 14 ms + 460 ms): after it the parts
// are done, and one mounted later (a new hour on a refresh) stands at once
// instead of playing alone.
export const REVEAL_MS = 2000;

type Entry = { mounted: RefObject<number | null>; settle: number };
const TileEntryContext = createContext<Entry | null>(null);

// TileEntry is a tile entering: its cards play no earlier than settle ms
// after the tile mounted, not under the tile still rising in. A card mounted
// later (a forecast that gains its rain) waits for nothing.
export function TileEntry({
  settle,
  children,
}: {
  settle: number;
  children: ReactNode;
}) {
  const mounted = useRef<number | null>(null);
  useEffect(() => {
    mounted.current = Date.now();
  }, []);
  // Stable: a new value would start every card's observer over.
  const entry = useMemo(() => ({ mounted, settle }), [settle]);
  return (
    <TileEntryContext.Provider value={entry}>
      {children}
    </TileEntryContext.Provider>
  );
}

function isSeen(entry: IntersectionObserverEntry): boolean {
  if (entry.intersectionRatio >= SEEN) return true;
  const view = entry.rootBounds?.height ?? 0;
  return view > 0 && entry.intersectionRect.height >= SEEN * view;
}

// useFirstSight tells whether the element has been seen (isSeen), once per
// mount, and not before its tile stands (TileEntry). The carousel keeps
// every card mounted, so a reveal on mount would mostly play unseen. No
// observer: seen at once. Not enabled: nothing is watched, and an element
// enabled later is still seen on its own.
export function useFirstSight<T extends Element>(enabled = true) {
  // A callback ref: an element that mounts later is still watched.
  const [node, ref] = useState<T | null>(null);
  const tile = useContext(TileEntryContext);
  const [seen, setSeen] = useState(
    () => typeof IntersectionObserver === "undefined",
  );
  useEffect(() => {
    if (seen || !enabled || node === null) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some(isSeen)) return;
        observer.disconnect();
        // No mount time yet: the tile mounts with the card, all of it ahead.
        const since = tile?.mounted.current ?? null;
        const wait =
          tile === null
            ? 0
            : since === null
              ? tile.settle
              : tile.settle - (Date.now() - since);
        if (wait <= 0) setSeen(true);
        else timer = setTimeout(() => setSeen(true), wait);
      },
      { threshold: THRESHOLDS },
    );
    observer.observe(node);
    return () => {
      observer.disconnect();
      clearTimeout(timer);
    };
  }, [node, seen, enabled, tile]);
  return { ref, seen };
}

// useReveal is the props that play a card's reveal-* parts (index.css) on
// its first sight, spread on the card's root: they wait at their start,
// run once seen, and are done REVEAL_MS later.
export function useReveal<T extends Element>() {
  const { ref, seen } = useFirstSight<T>();
  const [done, setDone] = useState(false);
  useEffect(() => {
    if (!seen) return;
    const timer = setTimeout(() => setDone(true), REVEAL_MS);
    return () => clearTimeout(timer);
  }, [seen]);
  const state = done ? "done" : seen ? "run" : "wait";
  return { ref, "data-reveal": state } as const;
}

// stagger is the inline delay of the i-th part, step ms after the one
// before, and after ms more.
export function stagger(i: number, step: number, after = 0): CSSProperties {
  return { animationDelay: `${i * step + after}ms` };
}

// swingFrom is how far the wind arrow, turned 0..359° from north, turns back
// to start at north the short way round: at 300° it swings in from 60° to
// its left.
export function swingFrom(turn: number): number {
  return turn > 180 ? 360 - turn : -turn;
}
