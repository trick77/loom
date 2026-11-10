import { useCallback, useEffect, useRef, useState } from "react";

import { useMediaQuery } from "../chat/useMediaQuery";

// STEP_MS is the beat: the tiles turn by themselves in turn, one per beat,
// so each card shows two beats and the two never turn together.
export const STEP_MS = 15_000;

export type Tile = 0 | 1;

// useTileClock turns two tiles' cards on one beat: the left at the first,
// the right at the second, the left again at the third. A tile with one card
// lets its beat pass, so a lone turning tile turns every two. A turn by hand
// starts the beat over with the other tile due: the turned card shows two
// beats, the other tile turns one beat later. It holds still while a tile is
// read (mouse over it, keyboard focus in it), on a hidden page, and never
// turns for reduced motion.
export function useTileClock(counts: readonly [number, number]) {
  const [index, setIndex] = useState<[number, number]>([0, 0]);
  const [due, setDue] = useState<Tile>(0);
  // epoch counts the turns by hand: each starts the beat over.
  const [epoch, setEpoch] = useState(0);
  const [held, setHeld] = useState<[boolean, boolean]>([false, false]);
  const [hidden, setHidden] = useState(() => document.hidden);
  const reduced = useMediaQuery("(prefers-reduced-motion: reduce)");
  useEffect(() => {
    const onChange = () => setHidden(document.hidden);
    document.addEventListener("visibilitychange", onChange);
    return () => document.removeEventListener("visibilitychange", onChange);
  }, []);

  const [left, right] = counts;
  const auto = (left > 1 || right > 1) && !reduced;
  const running = auto && !held[0] && !held[1] && !hidden;
  // The beat reads the counts when it strikes: a refresh that adds or drops
  // a card keeps the beat (and the bars) where they are.
  const countsRef = useRef<readonly [number, number]>(counts);
  useEffect(() => {
    countsRef.current = [left, right];
  }, [left, right]);
  useEffect(() => {
    if (!running) return;
    const timer = setTimeout(() => {
      const count = countsRef.current[due];
      if (count > 1) {
        setIndex((prev) => {
          const out: [number, number] = [prev[0], prev[1]];
          out[due] = (Math.min(prev[due], count - 1) + 1) % count;
          return out;
        });
      }
      setDue(due === 0 ? 1 : 0);
    }, STEP_MS);
    return () => clearTimeout(timer);
  }, [running, due, epoch]);

  const turn = useCallback((tile: Tile, to: number) => {
    setIndex((prev) => {
      const out: [number, number] = [prev[0], prev[1]];
      out[tile] = to;
      return out;
    });
    setDue(tile === 0 ? 1 : 0);
    setEpoch((e) => e + 1);
  }, []);

  // One stable callback per tile: a tile reports from an effect on it.
  const holdLeft = useCallback(
    (on: boolean) => setHeld((prev) => (prev[0] === on ? prev : [on, prev[1]])),
    [],
  );
  const holdRight = useCallback(
    (on: boolean) => setHeld((prev) => (prev[1] === on ? prev : [prev[0], on])),
    [],
  );

  return { index, due, epoch, auto, running, turn, holdLeft, holdRight };
}
