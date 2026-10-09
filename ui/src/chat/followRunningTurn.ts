import {
  attachStream,
  StreamInterruptedError,
  type StreamHandlers,
} from "../api";

// How many reattaches are tried before the turn is reported as interrupted.
const MAX_ATTACH_ATTEMPTS = 3;

// followRunningTurn reattaches to a thread's turn that is still running on the
// server, typically after a phone froze the tab and dropped the stream. It
// waits until the page is visible and online again, then follows the turn; a
// reattach the network drops as well is retried. Each attempt gets fresh
// handlers, because the server replays the turn from its first event.
export async function followRunningTurn(opts: {
  threadId: string;
  signal: AbortSignal;
  handlers: () => StreamHandlers;
  retryDelayMs?: number;
}): Promise<"attached" | "finished"> {
  const retryDelayMs = opts.retryDelayMs ?? 1000;
  for (let attempt = 1; ; attempt++) {
    await whenReachable(opts.signal);
    try {
      return await attachStream(opts.threadId, opts.handlers(), opts.signal);
    } catch (error) {
      // fetch rejects with a TypeError when the network is down.
      const dropped =
        error instanceof StreamInterruptedError || error instanceof TypeError;
      if (!dropped) throw error;
      if (attempt >= MAX_ATTACH_ATTEMPTS) throw new StreamInterruptedError();
    }
    await sleep(retryDelayMs * attempt, opts.signal);
  }
}

function reachable(): boolean {
  return document.visibilityState !== "hidden" && navigator.onLine !== false;
}

function abortError(): DOMException {
  return new DOMException("aborted", "AbortError");
}

// whenReachable resolves once the page is visible and online: a hidden tab on
// a phone is frozen, so a request from it would only fail again.
function whenReachable(signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.reject(abortError());
  if (reachable()) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      document.removeEventListener("visibilitychange", check);
      window.removeEventListener("online", check);
      signal.removeEventListener("abort", onAbort);
    };
    const check = () => {
      if (!reachable()) return;
      cleanup();
      resolve();
    };
    const onAbort = () => {
      cleanup();
      reject(abortError());
    };
    document.addEventListener("visibilitychange", check);
    window.addEventListener("online", check);
    signal.addEventListener("abort", onAbort);
  });
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.reject(abortError());
  return new Promise((resolve, reject) => {
    const onAbort = () => {
      window.clearTimeout(timer);
      reject(abortError());
    };
    const timer = window.setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}
