import { afterEach, describe, expect, test, vi } from "vitest";

import { StreamInterruptedError } from "../api";
import { followRunningTurn } from "./followRunningTurn";

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

  test("gives up as interrupted after three dropped attempts", async () => {
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
    expect(fetchMock).toHaveBeenCalledTimes(3);
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
