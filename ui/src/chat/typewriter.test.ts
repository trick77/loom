import { afterEach, describe, expect, it, vi } from "vitest";

import {
  MAX_GAP_MS,
  MIN_CHARS_PER_SEC,
  SETTLE_MS,
  createTypewriter,
  cutAt,
  shouldPace,
  type TypewriterClock,
} from "./typewriter";

// fakeClock runs animation frames by hand at 60fps.
function fakeClock() {
  let now = 0;
  let next = 1;
  const frames = new Map<number, (now: number) => void>();
  const clock: TypewriterClock = {
    requestFrame: (callback) => {
      frames.set(next, callback);
      return next++;
    },
    cancelFrame: (handle) => {
      frames.delete(handle);
    },
    now: () => now,
    after: (ms, callback) => {
      const handle = setTimeout(callback, ms);
      return () => clearTimeout(handle);
    },
  };
  const frame = () => {
    now += 1000 / 60;
    const due = [...frames.values()];
    frames.clear();
    for (const callback of due) callback(now);
  };
  return { clock, frame, pending: () => frames.size };
}

function writer() {
  const { clock, frame, pending } = fakeClock();
  let shown = "";
  const emits: string[] = [];
  const typewriter = createTypewriter((text) => {
    shown += text;
    emits.push(text);
  }, clock);
  return { typewriter, frame, pending, shown: () => shown, emits };
}

const PARAGRAPH = Array.from({ length: 80 }, (_, i) => `word${i}`).join(" ");

describe("cutAt", () => {
  it("moves the cut to just after the next whitespace", () => {
    expect(cutAt("hello world again", 3)).toBe(6);
  });
  it("cuts at the budget when no whitespace is near", () => {
    const run = "长".repeat(60);
    expect(cutAt(run, 5)).toBe(5);
  });
  it("never splits a surrogate pair", () => {
    const text = "😀".repeat(40);
    expect(cutAt(text, 3) % 2).toBe(0);
  });
  it("releases everything once the budget covers the text", () => {
    expect(cutAt("short", 99)).toBe(5);
    expect(cutAt("short", 0)).toBe(0);
  });
});

describe("createTypewriter", () => {
  it("types a paragraph-sized delta out over several frames, in word steps", () => {
    const { typewriter, frame, shown, emits } = writer();
    typewriter.push(PARAGRAPH);
    expect(shown()).toBe("");
    frame();
    expect(shown().length).toBeGreaterThan(0);
    expect(shown().length).toBeLessThan(PARAGRAPH.length / 4);
    for (let i = 0; i < 120; i += 1) frame();
    expect(shown()).toBe(PARAGRAPH);
    // Every cut but the last ends a word.
    for (const piece of emits.slice(0, -1)) expect(piece).toMatch(/\s$/);
  });

  it("spreads a burst over the learned gap between arrivals", () => {
    const { typewriter, frame, shown } = writer();
    const burst = PARAGRAPH.slice(0, 400);
    const gapFrames = 162; // ~2.7s, a bursty model measured through loom
    let finishedAt = -1;
    for (let arrival = 0; arrival < 4; arrival += 1) {
      const before = shown().length;
      typewriter.push(burst);
      for (let i = 0; i < gapFrames; i += 1) {
        frame();
        if (arrival === 3 && finishedAt < 0 && shown().length === before + 400)
          finishedAt = i;
      }
    }
    // Stretched across most of the gap rather than typed out in a blink and
    // then stalled.
    expect(finishedAt).toBeGreaterThan(gapFrames * 0.6);
  });

  it("puts a trickle up almost at once", () => {
    const { typewriter, frame, shown } = writer();
    let sent = "";
    for (let i = 0; i < 60; i += 1) {
      typewriter.push("tok ");
      sent += "tok ";
      frame();
      frame();
    }
    expect(sent.length - shown().length).toBeLessThanOrEqual(8);
  });

  it("never trails by more than the gap cap", () => {
    const { typewriter, frame, shown } = writer();
    typewriter.push("a ");
    for (let i = 0; i < 600; i += 1) frame(); // a 10s stall learns a long gap
    typewriter.push(PARAGRAPH);
    for (let i = 0; i < Math.ceil(MAX_GAP_MS / (1000 / 60)) + 1; i += 1)
      frame();
    expect(shown()).toBe("a " + PARAGRAPH);
  });

  it("keeps a minimum pace on a few words", () => {
    const { typewriter, frame, shown } = writer();
    typewriter.push("a ".repeat(MIN_CHARS_PER_SEC));
    for (let i = 0; i < 60; i += 1) frame();
    expect(shown().length).toBeGreaterThanOrEqual(MIN_CHARS_PER_SEC * 2 - 2);
  });

  it("settle types the tail out before the deadline, then calls back", () => {
    const { typewriter, frame, shown } = writer();
    const big = PARAGRAPH.repeat(20);
    typewriter.push(big);
    const done = vi.fn();
    typewriter.settle(done);
    expect(typewriter.idle()).toBe(false);
    frame();
    expect(done).not.toHaveBeenCalled();
    const frames = Math.ceil(SETTLE_MS / (1000 / 60)) + 1;
    for (let i = 0; i < frames; i += 1) frame();
    expect(shown()).toBe(big);
    expect(done).toHaveBeenCalledOnce();
    expect(typewriter.idle()).toBe(true);
  });

  it("settle still finishes when no frame ever runs (a hidden tab)", () => {
    vi.useFakeTimers();
    try {
      const { typewriter, shown } = writer();
      typewriter.push(PARAGRAPH);
      const done = vi.fn();
      typewriter.settle(done);
      vi.advanceTimersByTime(SETTLE_MS + 100);
      expect(shown()).toBe(PARAGRAPH);
      expect(done).toHaveBeenCalledOnce();
    } finally {
      vi.useRealTimers();
    }
  });

  it("settle on an empty backlog calls back at once", () => {
    const { typewriter } = writer();
    const done = vi.fn();
    typewriter.settle(done);
    expect(done).toHaveBeenCalledOnce();
  });

  it("flush shows everything now, cancels the frame and runs the settle", () => {
    const { typewriter, pending, shown } = writer();
    typewriter.push(PARAGRAPH);
    const done = vi.fn();
    typewriter.settle(done);
    typewriter.flush();
    expect(shown()).toBe(PARAGRAPH);
    expect(done).toHaveBeenCalledOnce();
    expect(pending()).toBe(0);
  });

  it("ignores an empty push", () => {
    const { typewriter, pending } = writer();
    typewriter.push("");
    expect(pending()).toBe(0);
    expect(typewriter.idle()).toBe(true);
  });
});

describe("shouldPace", () => {
  afterEach(() => {
    // @ts-expect-error jsdom has no matchMedia; restore that.
    delete window.matchMedia;
  });
  it("is off without matchMedia", () => {
    expect(shouldPace()).toBe(false);
  });
  it("follows prefers-reduced-motion", () => {
    window.matchMedia = vi.fn().mockReturnValue({
      matches: true,
    }) as unknown as typeof window.matchMedia;
    expect(shouldPace()).toBe(false);
    window.matchMedia = vi.fn().mockReturnValue({
      matches: false,
    }) as unknown as typeof window.matchMedia;
    expect(shouldPace()).toBe(true);
  });
});
