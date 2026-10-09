import { afterEach, describe, expect, test, vi } from "vitest";

import { StreamInterruptedError } from "../api";
import { MAX_ATTACH_ATTEMPTS, followRunningTurn } from "./followRunningTurn";

function sseResponse(events: string[]) {
  const encoder = new TextEncoder();
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        for (const event of events) controller.enqueue(encoder.encode(event));
        controller.close();
      },
    }),
  );
}

const finishedTurn = [
  'event: assistant_delta\ndata: {"content":"Hello"}\n\n',
  'event: assistant_message\ndata: {"id":"m2","content":"Hello"}\n\n',
  "event: done\ndata: {}\n\n",
];

function handlers() {
  return {
    onUserMessage: vi.fn(),
    onDelta: vi.fn(),
    onAssistantMessage: vi.fn(),
    onThread: vi.fn(),
  };
}

let visibility: DocumentVisibilityState = "visible";
Object.defineProperty(document, "visibilityState", {
  configurable: true,
  get: () => visibility,
});

afterEach(() => {
  visibility = "visible";
  vi.unstubAllGlobals();
});

describe("followRunningTurn", () => {
  test("reattaches at once while the page is visible", async () => {
    const fetchMock = vi.fn().mockResolvedValue(sseResponse(finishedTurn));
    vi.stubGlobal("fetch", fetchMock);
    const h = handlers();

    await expect(
      followRunningTurn({
        threadId: "t1",
        signal: new AbortController().signal,
        handlers: () => h,
      }),
    ).resolves.toBe("attached");
    expect(h.onDelta).toHaveBeenCalledWith("Hello");
  });

  test("waits for the page to become visible again", async () => {
    visibility = "hidden";
    const fetchMock = vi.fn().mockResolvedValue(sseResponse(finishedTurn));
    vi.stubGlobal("fetch", fetchMock);

    const followed = followRunningTurn({
      threadId: "t1",
      signal: new AbortController().signal,
      handlers,
    });
    await Promise.resolve();
    expect(fetchMock).not.toHaveBeenCalled();

    visibility = "visible";
    document.dispatchEvent(new Event("visibilitychange"));
    await expect(followed).resolves.toBe("attached");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  test("retries a reattach the network dropped, with fresh handlers", async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("Load failed"))
      .mockResolvedValueOnce(sseResponse(finishedTurn));
    vi.stubGlobal("fetch", fetchMock);
    const created: ReturnType<typeof handlers>[] = [];

    await expect(
      followRunningTurn({
        threadId: "t1",
        signal: new AbortController().signal,
        handlers: () => {
          const h = handlers();
          created.push(h);
          return h;
        },
        retryDelayMs: 0,
      }),
    ).resolves.toBe("attached");
    expect(created).toHaveLength(2);
    expect(created[1].onAssistantMessage).toHaveBeenCalled();
  });

  test("gives up as interrupted once every attempt dropped", async () => {
    const fetchMock = vi.fn().mockRejectedValue(new TypeError("Load failed"));
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      followRunningTurn({
        threadId: "t1",
        signal: new AbortController().signal,
        handlers,
        retryDelayMs: 0,
      }),
    ).rejects.toBeInstanceOf(StreamInterruptedError);
    expect(fetchMock).toHaveBeenCalledTimes(MAX_ATTACH_ATTEMPTS);
  });

  // A bug in a handler is no dropped connection: retrying it for 45s and
  // then reporting an interruption would hide it.
  test("a handler that throws is not retried", async () => {
    const fetchMock = vi.fn().mockResolvedValue(sseResponse(finishedTurn));
    vi.stubGlobal("fetch", fetchMock);
    const bug = new TypeError("cannot read properties of undefined");

    await expect(
      followRunningTurn({
        threadId: "t1",
        signal: new AbortController().signal,
        handlers: () => ({
          ...handlers(),
          onDelta: () => {
            throw bug;
          },
        }),
        retryDelayMs: 0,
      }),
    ).rejects.toBe(bug);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  // Every attach that worked gives the next drop a fresh budget: a long turn
  // on a phone that is backgrounded again and again must not run out.
  test("an attach that streamed resets the retry budget", async () => {
    const dropped = () =>
      sseResponse(['event: assistant_delta\ndata: {"content":"a"}\n\n']);
    const fetchMock = vi.fn();
    for (let i = 0; i < MAX_ATTACH_ATTEMPTS + 2; i++)
      fetchMock.mockResolvedValueOnce(dropped());
    fetchMock.mockResolvedValueOnce(sseResponse(finishedTurn));
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      followRunningTurn({
        threadId: "t1",
        signal: new AbortController().signal,
        handlers,
        retryDelayMs: 0,
        progressAfterMs: 0,
      }),
    ).resolves.toBe("attached");
    expect(fetchMock).toHaveBeenCalledTimes(MAX_ATTACH_ATTEMPTS + 3);
  });

  // Every attach replays the turn from its first event, so events alone prove
  // nothing: a link that keeps cutting out right after the replay must still
  // run out of attempts instead of looping for the whole turn.
  test("attaches that drop right after the replay still use up the budget", async () => {
    const fetchMock = vi.fn(async () =>
      sseResponse(['event: assistant_delta\ndata: {"content":"a"}\n\n']),
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      followRunningTurn({
        threadId: "t1",
        signal: new AbortController().signal,
        handlers,
        retryDelayMs: 0,
      }),
    ).rejects.toBeInstanceOf(StreamInterruptedError);
    expect(fetchMock).toHaveBeenCalledTimes(MAX_ATTACH_ATTEMPTS);
  });

  test("reports a turn that finished meanwhile", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 204 })),
    );

    await expect(
      followRunningTurn({
        threadId: "t1",
        signal: new AbortController().signal,
        handlers,
      }),
    ).resolves.toBe("finished");
  });

  test("a stop while waiting ends it as an abort", async () => {
    visibility = "hidden";
    vi.stubGlobal("fetch", vi.fn());
    const controller = new AbortController();

    const followed = followRunningTurn({
      threadId: "t1",
      signal: controller.signal,
      handlers,
    });
    controller.abort();
    await expect(followed).rejects.toMatchObject({ name: "AbortError" });
  });
});
