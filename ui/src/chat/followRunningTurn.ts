import {
  attachStream,
  StreamInterruptedError,
  type StreamHandlers,
} from "../api";

// How many reattaches are tried before the turn is reported as interrupted.
// With the backoff below that is about 45s: a phone coming back from the
// background can take several seconds to get its network back.
export const MAX_ATTACH_ATTEMPTS = 8;
// The backoff doubles from retryDelayMs and stops growing at this multiple.
const MAX_BACKOFF_FACTOR = 10;

// followRunningTurn reattaches to a thread's turn that is still running on the
// server, typically after a phone froze the tab and dropped the stream. It
// waits until the page is visible and online again, then follows the turn; a
// reattach the network drops as well is retried, and one that streamed before
// it dropped starts the retry budget afresh. Each attempt gets fresh handlers,
// because the server replays the turn from its first event.
export async function followRunningTurn(opts: {
  threadId: string;
  signal: AbortSignal;
  handlers: () => StreamHandlers;
  retryDelayMs?: number;
}): Promise<"attached" | "finished"> {
  const retryDelayMs = opts.retryDelayMs ?? 1000;
  for (let attempt = 1; ; attempt++) {
    await whenReachable(opts.signal);
    let streamed = false;
    try {
      return await attachStream(
        opts.threadId,
        noteProgress(opts.handlers(), () => (streamed = true)),
        opts.signal,
      );
    } catch (error) {
      if (!(error instanceof StreamInterruptedError)) throw error;
      // The loop's increment makes the next attempt the first again.
      if (streamed) attempt = 0;
      else if (attempt >= MAX_ATTACH_ATTEMPTS) throw error;
    }
    const factor = Math.min(2 ** Math.max(attempt - 1, 0), MAX_BACKOFF_FACTOR);
    await sleep(retryDelayMs * factor, opts.signal);
  }
}

// noteProgress wraps every handler so onEvent runs first: an attach that
// delivered events worked, whatever ends it.
function noteProgress(
  handlers: StreamHandlers,
  onEvent: () => void,
): StreamHandlers {
  const wrapped: Record<string, unknown> = {};
  for (const [name, handler] of Object.entries(handlers)) {
    wrapped[name] =
      typeof handler === "function"
        ? (...args: unknown[]) => {
            onEvent();
            return (handler as (...a: unknown[]) => unknown)(...args);
          }
        : handler;
  }
  return wrapped as StreamHandlers;
}

function reachable(): boolean {
  return document.visibilityState !== "hidden" && navigator.onLine !== false;
}

function abortError(): DOMException {
  return new DOMException("aborted", "AbortError");
}

// whenReachable resolves once the page is visible and online: a hidden tab on
// a phone is frozen, so a request from it would only fail again.
export function whenReachable(signal: AbortSignal): Promise<void> {
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
