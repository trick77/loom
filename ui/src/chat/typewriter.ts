// Typewriter pacing for streamed answer text.
//
// How an answer arrives depends on the model and the path to it, not on us.
// One model arrives as a steady trickle of token-sized deltas. Another, measured
// through loom in the browser, arrives as ~80 deltas (~400 chars) landing in the
// same instant, then ~2.7s of nothing, then the next burst: appended as they
// come, the answer jumps a paragraph at a time. The typewriter sits between the
// delta handler and the text block, holds the backlog and releases it a few
// characters per animation frame, cut at word ends.
//
// The rate is aimed at the NEXT arrival: the gap between arrivals is learned as
// they come, and the backlog is spread to run out as the next one is due. A
// fixed rate cannot serve both models: fast enough for bursts it types a
// burst out and stalls until the next, slow enough to fill the gap it lags a
// trickle. Aimed at the arrival, a trickle's ~20ms gaps put text up almost at once
// and bursts become one steady line.
//
// Pacing only ever DELAYS text, never drops it, and never by more than
// MAX_GAP_MS. Once the stream has ended (settle) the tail is typed out against
// SETTLE_MS instead of dumped.

// BURST_MS joins deltas arriving this close together into one arrival: a burst
// dispatches its deltas within the same few milliseconds, while a trickle's are
// ~20ms apart and must stay separate arrivals, or its gap is never learned.
const BURST_MS = 15;
// FIRST_GAP_MS is the assumed gap before one has been measured.
const FIRST_GAP_MS = 600;
// MAX_GAP_MS caps the learned gap, and so how far typing may trail arrival: a
// stall (a tool round, a slow token) must not stretch the words before it.
export const MAX_GAP_MS = 3000;
// MIN_CHARS_PER_SEC stops a few words from crawling across a long gap.
export const MIN_CHARS_PER_SEC = 60;
// SETTLE_MS is the deadline for typing out the tail once the stream has ended. A
// fast model often finishes before a word is on screen; flushing there would
// skip the effect entirely, holding the end longer would stall the turn.
export const SETTLE_MS = 1500;
// SNAP_CHARS is how far a cut may move forward to land after whitespace. Past it
// (a long URL, a CJK run with no spaces) the cut falls where the rate put it.
const SNAP_CHARS = 24;
const FRAME_MS = 1000 / 60;

export type TypewriterClock = {
  requestFrame: (callback: (now: number) => void) => number;
  cancelFrame: (handle: number) => void;
  now: () => number;
  // after runs callback once in ms and returns its cancel.
  after: (ms: number, callback: () => void) => () => void;
};

const browserClock: TypewriterClock = {
  requestFrame: (callback) => requestAnimationFrame(callback),
  cancelFrame: (handle) => cancelAnimationFrame(handle),
  now: () => performance.now(),
  after: (ms, callback) => {
    const handle = setTimeout(callback, ms);
    return () => clearTimeout(handle);
  },
};

// shouldPace says whether answers are typed out: always in a browser,
// whatever the OS motion setting says. Off under the test runner, where text
// lands synchronously as it always did and the typewriter is tested directly.
export function shouldPace(): boolean {
  return (
    import.meta.env.MODE !== "test" &&
    typeof window !== "undefined" &&
    typeof window.requestAnimationFrame === "function"
  );
}

// cutAt returns how many characters of text to release for a budget of want:
// the budget moved forward to just after the next whitespace, or to the end of
// the text, when either is near; never splitting a surrogate pair.
export function cutAt(text: string, want: number): number {
  if (want >= text.length) return text.length;
  if (want <= 0) return 0;
  if (text.length - want <= SNAP_CHARS) {
    const space = text.slice(want).search(/\s/);
    return space === -1 ? text.length : want + space + 1;
  }
  for (let index = want; index < want + SNAP_CHARS; index += 1) {
    if (/\s/.test(text[index])) return index + 1;
  }
  const code = text.charCodeAt(want - 1);
  if (code >= 0xd800 && code <= 0xdbff) return want + 1;
  return want;
}

export type Typewriter = {
  push: (text: string) => void;
  // settle runs done once the backlog has been typed out, within SETTLE_MS.
  settle: (done: () => void) => void;
  // flush releases the whole backlog now and runs a pending settle callback.
  flush: () => void;
  idle: () => boolean;
};

export function createTypewriter(
  emit: (text: string) => void,
  clock: TypewriterClock = browserClock,
): Typewriter {
  let backlog = "";
  let credit = 0;
  let lastFrame: number | null = null;
  let frame: number | null = null;
  // arrivedAt is when the latest arrival began, gap the learned time between
  // arrivals: together they say when the backlog should have run out.
  let arrivedAt: number | null = null;
  let lastPush = -Infinity;
  let gap = FIRST_GAP_MS;
  let settleBy: number | null = null;
  let onSettled: (() => void) | null = null;
  let cancelBackstop: (() => void) | null = null;

  const finishSettle = () => {
    const done = onSettled;
    onSettled = null;
    settleBy = null;
    cancelBackstop?.();
    cancelBackstop = null;
    done?.();
  };

  const release = (count: number) => {
    const text = backlog.slice(0, count);
    backlog = backlog.slice(count);
    if (text !== "") emit(text);
  };

  const tick = (now: number) => {
    frame = null;
    const elapsed = lastFrame === null ? FRAME_MS : now - lastFrame;
    lastFrame = now;
    // Resized every frame against the time left, which converges: the window
    // shrinks as fast as the backlog does, and a late frame catches up.
    let due = (arrivedAt ?? now) + gap;
    if (settleBy !== null) due = Math.min(due, settleBy);
    const rate = Math.max(
      MIN_CHARS_PER_SEC,
      (backlog.length * 1000) / Math.max(due - now, FRAME_MS),
    );
    credit += (rate * elapsed) / 1000;
    const count = cutAt(backlog, Math.floor(credit));
    credit -= count;
    release(count);
    if (backlog !== "") {
      schedule();
      return;
    }
    credit = 0;
    lastFrame = null;
    if (onSettled !== null) finishSettle();
  };

  const schedule = () => {
    if (frame === null) frame = clock.requestFrame(tick);
  };

  const api: Typewriter = {
    push(text) {
      if (text === "") return;
      const now = clock.now();
      if (now - lastPush > BURST_MS) {
        if (arrivedAt !== null)
          gap = Math.min(MAX_GAP_MS, (gap + (now - arrivedAt)) / 2);
        arrivedAt = now;
      }
      lastPush = now;
      backlog += text;
      schedule();
    },
    settle(done) {
      onSettled = done;
      if (backlog === "") {
        finishSettle();
        return;
      }
      settleBy = clock.now() + SETTLE_MS;
      schedule();
      // A hidden tab gets no animation frames, and a turn waiting on them
      // would stay streaming until the tab came back. The timer still fires
      // there (throttled), so the deadline holds either way.
      cancelBackstop = clock.after(SETTLE_MS + 100, () => api.flush());
    },
    flush() {
      if (frame !== null) clock.cancelFrame(frame);
      frame = null;
      credit = 0;
      lastFrame = null;
      release(backlog.length);
      if (onSettled !== null) finishSettle();
    },
    idle: () => backlog === "" && onSettled === null,
  };
  return api;
}
