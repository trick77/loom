import "@testing-library/jest-dom/vitest";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import "../i18n";
import type { TodaySun } from "../api/today";
import { SWEEP_MS, SunCard, sunPoint, sweepAt } from "./SunCard";

const sun: TodaySun = {
  rise: "07:25",
  set: "16:58",
  dayMinutes: 573,
  polar: "",
  progress: 0.4,
};

// The observer and the frames are the test's: it says when the card comes
// into view and when a frame is painted.
let seen: ((entries: Partial<IntersectionObserverEntry>[]) => void) | null;
let observers: number;
let frames: FrameRequestCallback[];
// asked counts every frame asked for: its id.
let asked: number;
let canceled: number[];

beforeEach(() => {
  seen = null;
  observers = 0;
  frames = [];
  asked = 0;
  canceled = [];
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(
        callback: (entries: Partial<IntersectionObserverEntry>[]) => void,
      ) {
        seen = callback;
        observers++;
      }
      observe() {}
      disconnect() {
        seen = null;
      }
    },
  );
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
    frames.push(callback);
    return ++asked;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => {
    canceled.push(id);
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function paint(at: number) {
  const due = frames;
  frames = [];
  act(() => due.forEach((frame) => frame(at)));
}

// inView reports the share of the card in view, as the observer does: any
// overlap intersects.
function inView(ratio: number) {
  act(() => seen?.([{ isIntersecting: ratio > 0, intersectionRatio: ratio }]));
}

function dot() {
  const el = screen.getByTestId("sun-dot");
  return { x: Number(el.getAttribute("cx")), y: Number(el.getAttribute("cy")) };
}

test("sweepAt eases from sunrise to the target", () => {
  expect(sweepAt(0.4, 0)).toBe(0);
  expect(sweepAt(0.4, SWEEP_MS / 2)).toBeGreaterThan(0.2);
  expect(sweepAt(0.4, SWEEP_MS / 2)).toBeLessThan(0.4);
  expect(sweepAt(0.4, SWEEP_MS)).toBe(0.4);
  expect(sweepAt(0.4, SWEEP_MS * 3)).toBe(0.4);
  expect(sweepAt(0.4, -5)).toBe(0);
});

test("the sun runs its course once the card is seen, and only once", () => {
  render(<SunCard day="today" sun={sun} />);
  // Unseen it waits at sunrise.
  expect(dot()).toEqual(sunPoint(0));
  inView(0);
  // A sliver in view is not seen.
  inView(0.05);
  expect(frames).toHaveLength(0);
  inView(0.8);
  // Seen: the observer is done, the frames run.
  expect(seen).toBeNull();
  paint(1000);
  paint(1000 + SWEEP_MS / 2);
  const half = dot();
  expect(half.x).toBeGreaterThan(sunPoint(0).x);
  expect(half.x).toBeLessThan(sunPoint(0.4).x);
  paint(1000 + SWEEP_MS);
  expect(dot()).toEqual(sunPoint(0.4));
  // Done: no further frame, no new observer.
  expect(frames).toHaveLength(0);
  expect(observers).toBe(1);
});

test("reduced motion runs the course all the same", () => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn().mockImplementation((query: string) => ({
      matches: query.includes("reduce"),
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
  render(<SunCard day="today" sun={sun} />);
  expect(dot()).toEqual(sunPoint(0));
  inView(1);
  paint(0);
  paint(SWEEP_MS);
  expect(dot()).toEqual(sunPoint(0.4));
});

test("without an observer the sun stands where it is", () => {
  vi.stubGlobal("IntersectionObserver", undefined);
  render(<SunCard day="today" sun={sun} />);
  expect(dot()).toEqual(sunPoint(0.4));
});

test("a polar day sweeps to noon; a set sun waits for nothing", () => {
  const { unmount } = render(
    <SunCard day="today" sun={{ ...sun, polar: "day", progress: null }} />,
  );
  inView(1);
  paint(0);
  paint(SWEEP_MS);
  expect(dot()).toEqual(sunPoint(0.5));
  unmount();
  observers = 0;
  render(<SunCard day="today" sun={{ ...sun, progress: 1.3 }} />);
  expect(screen.queryByTestId("sun-dot")).toBeNull();
  expect(observers).toBe(0);
});

test("a sun that rises while the page is open still runs its course", () => {
  const { rerender } = render(
    <SunCard day="today" sun={{ ...sun, progress: -0.01 }} />,
  );
  expect(observers).toBe(0);
  rerender(<SunCard day="today" sun={{ ...sun, progress: 0.01 }} />);
  expect(observers).toBe(1);
  inView(1);
  paint(0);
  paint(SWEEP_MS);
  expect(dot()).toEqual(sunPoint(0.01));
});

test("leaving mid-sweep cancels the next frame", () => {
  const { unmount } = render(<SunCard day="today" sun={sun} />);
  inView(1);
  paint(0);
  expect(frames).toHaveLength(1);
  // The frame just asked for is the one canceled, not the first.
  const pending = 2;
  unmount();
  expect(canceled).toEqual([pending]);
});
