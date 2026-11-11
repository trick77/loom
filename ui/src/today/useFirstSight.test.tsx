import "@testing-library/jest-dom/vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import {
  REVEAL_MS,
  SEEN,
  stagger,
  swingFrom,
  TileEntry,
  useFirstSight,
  useReveal,
} from "./useFirstSight";

// The observer is the test's: it says when the element comes into view.
let report: ((entries: Partial<IntersectionObserverEntry>[]) => void) | null;
let observers: number;

beforeEach(() => {
  report = null;
  observers = 0;
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(
        callback: (entries: Partial<IntersectionObserverEntry>[]) => void,
      ) {
        report = callback;
        observers++;
      }
      observe() {}
      disconnect() {
        report = null;
      }
    },
  );
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function inView(ratio: number) {
  act(() =>
    report?.([{ isIntersecting: ratio > 0, intersectionRatio: ratio }]),
  );
}

function Probe({ enabled = true }: { enabled?: boolean }) {
  const { ref, seen } = useFirstSight<HTMLDivElement>(enabled);
  return <div data-seen={seen ? "yes" : "no"} data-testid="probe" ref={ref} />;
}

const probe = () => screen.getByTestId("probe");

test("seen once enough of it is in view, and only once", () => {
  render(<Probe />);
  expect(probe()).toHaveAttribute("data-seen", "no");
  inView(SEEN / 2);
  expect(probe()).toHaveAttribute("data-seen", "no");
  inView(SEEN);
  expect(probe()).toHaveAttribute("data-seen", "yes");
  // Done: the observer let go, no new one.
  expect(report).toBeNull();
  expect(observers).toBe(1);
});

test("taller than the view, filling most of it counts as seen", () => {
  render(<Probe />);
  const rects = (shown: number) =>
    act(() =>
      report?.([
        {
          intersectionRatio: 0.3,
          intersectionRect: { height: shown } as DOMRectReadOnly,
          rootBounds: { height: 400 } as DOMRectReadOnly,
        },
      ]),
    );
  rects(200);
  expect(probe()).toHaveAttribute("data-seen", "no");
  rects(400 * SEEN);
  expect(probe()).toHaveAttribute("data-seen", "yes");
});

test("without an observer it counts as seen at once", () => {
  vi.stubGlobal("IntersectionObserver", undefined);
  render(<Probe />);
  expect(probe()).toHaveAttribute("data-seen", "yes");
});

test("not enabled, nothing is watched until it is", () => {
  const { rerender } = render(<Probe enabled={false} />);
  expect(observers).toBe(0);
  rerender(<Probe />);
  expect(observers).toBe(1);
  inView(1);
  expect(probe()).toHaveAttribute("data-seen", "yes");
});

function Late({ shown }: { shown: boolean }) {
  const { ref, seen } = useFirstSight<HTMLSpanElement>();
  return (
    <div data-seen={seen ? "yes" : "no"} data-testid="probe">
      {shown && <span ref={ref} />}
    </div>
  );
}

test("an element that mounts later is still watched", () => {
  const { rerender } = render(<Late shown={false} />);
  expect(observers).toBe(0);
  rerender(<Late shown />);
  expect(observers).toBe(1);
  inView(1);
  expect(probe()).toHaveAttribute("data-seen", "yes");
});

test("seen while its tile still enters, it waits until the tile stands", () => {
  vi.useFakeTimers();
  render(
    <TileEntry settle={300}>
      <Probe />
    </TileEntry>,
  );
  vi.advanceTimersByTime(100);
  inView(1);
  expect(probe()).toHaveAttribute("data-seen", "no");
  act(() => vi.advanceTimersByTime(199));
  expect(probe()).toHaveAttribute("data-seen", "no");
  act(() => vi.advanceTimersByTime(1));
  expect(probe()).toHaveAttribute("data-seen", "yes");
});

test("a card mounted after its tile stands waits for nothing", () => {
  vi.useFakeTimers();
  const { rerender } = render(<TileEntry settle={300}>{null}</TileEntry>);
  act(() => vi.advanceTimersByTime(400));
  rerender(
    <TileEntry settle={300}>
      <Probe />
    </TileEntry>,
  );
  inView(1);
  expect(probe()).toHaveAttribute("data-seen", "yes");
});

function Card() {
  return <div data-testid="card" {...useReveal<HTMLDivElement>()} />;
}

test("a card's parts wait until it is seen, run, then are done", () => {
  vi.useFakeTimers();
  render(<Card />);
  const card = screen.getByTestId("card");
  expect(card).toHaveAttribute("data-reveal", "wait");
  inView(1);
  expect(card).toHaveAttribute("data-reveal", "run");
  act(() => vi.advanceTimersByTime(REVEAL_MS));
  expect(card).toHaveAttribute("data-reveal", "done");
});

test("stagger delays each part a step after the one before", () => {
  expect(stagger(0, 40)).toEqual({ animationDelay: "0ms" });
  expect(stagger(3, 40)).toEqual({ animationDelay: "120ms" });
  expect(stagger(2, 45, 260)).toEqual({ animationDelay: "350ms" });
});

test("the wind arrow swings in from north the short way round", () => {
  expect(swingFrom(45)).toBe(-45);
  expect(swingFrom(180)).toBe(-180);
  expect(swingFrom(300)).toBe(60);
  expect(swingFrom(0) === 0).toBe(true);
});

test("unmounted while waiting, it sets nothing", () => {
  vi.useFakeTimers();
  const { unmount } = render(
    <TileEntry settle={300}>
      <Probe />
    </TileEntry>,
  );
  inView(1);
  unmount();
  expect(vi.getTimerCount()).toBe(0);
});
