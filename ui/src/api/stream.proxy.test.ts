import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import {
  PROBE_KNOWN_MS,
  PROBE_MS,
  StreamInterruptedError,
  StreamStalledError,
  WATCHDOG_MS,
  HEARTBEAT_MS,
  attachStream,
  resetStreamTransport,
  streamBuffered,
  streamMessage,
} from "./stream";

// A proxy that buffers: the stream's request is held, not even its headers come
// back, until the client aborts it.
function held(signal?: AbortSignal | null): Promise<Response> {
  return new Promise((_, reject) => {
    signal?.addEventListener("abort", () =>
      reject(new DOMException("aborted", "AbortError")),
    );
  });
}

// live is a stream the test feeds by hand.
function live(signal?: AbortSignal | null) {
  const encoder = new TextEncoder();
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      controller = c;
    },
  });
  signal?.addEventListener("abort", () =>
    controller.error(new DOMException("aborted", "AbortError")),
  );
  return {
    response: new Response(body),
    send: (chunk: string) => controller.enqueue(encoder.encode(chunk)),
    close: () => controller.close(),
  };
}

const userMessage = {
  event: "user_message",
  data: JSON.stringify({ id: "m1", content: "Hi" }),
};

// pollAnswer is a poll answer; by default the probe's: the turn registered,
// its user message in the log.
const pollAnswer = (over: Partial<Record<string, unknown>> = {}) =>
  Response.json({
    hub: "h1",
    sendId: "send_1",
    next: 1,
    done: false,
    events: [userMessage],
    ...over,
  });
const delta = (content: string) => ({
  event: "assistant_delta",
  data: JSON.stringify({ content }),
});
const answer = {
  event: "assistant_message",
  data: JSON.stringify({ id: "m2", content: "Hello" }),
};
const done = { event: "done", data: "{}" };

function handlers() {
  return {
    onUserMessage: vi.fn(),
    onDelta: vi.fn(),
    onAssistantMessage: vi.fn(),
    onThread: vi.fn(),
  };
}

// fakeNetwork routes the stream to stream() and every poll to the next of
// polls; it records the poll URLs.
function fakeNetwork(
  stream: (signal?: AbortSignal | null) => Promise<Response>,
  polls: (() => Response)[],
) {
  const pollURLs: string[] = [];
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if (url.includes("messages:poll")) {
      pollURLs.push(url);
      const next = polls.shift();
      return next ? Promise.resolve(next()) : held(init?.signal);
    }
    return stream(init?.signal);
  });
  vi.stubGlobal("fetch", fetch);
  return { pollURLs };
}

let hidden = false;
beforeEach(() => {
  vi.useFakeTimers();
  resetStreamTransport();
  hidden = false;
  Object.defineProperty(document, "hidden", {
    configurable: true,
    get: () => hidden,
  });
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  delete (document as { hidden?: boolean }).hidden;
});

describe("a send behind a buffering proxy", () => {
  test("is proven held when its turn is registered while the stream has nothing", async () => {
    const net = fakeNetwork(held, [() => pollAnswer()]);
    const sent = streamMessage("t1", "Hi", handlers(), undefined, {
      clientMessageId: "send_1",
    }).catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(PROBE_MS);
    const error = await sent;
    expect(error).toBeInstanceOf(StreamStalledError);
    expect(error).toBeInstanceOf(StreamInterruptedError);
    expect(streamBuffered()).toBe(true);
    expect(net.pollURLs).toEqual(["/api/threads/t1/messages:poll"]);
  });

  test("is not broken off without proof: not registered yet, or another send's turn", async () => {
    let stream!: ReturnType<typeof live>;
    fakeNetwork(
      (signal) => {
        stream = live(signal);
        return Promise.resolve(stream.response);
      },
      [
        () => new Response(null, { status: 204 }),
        () => pollAnswer({ sendId: "send_other" }),
      ],
    );
    // the response came, its body has nothing yet
    const h = handlers();
    const sent = streamMessage("t1", "Hi", h, undefined, {
      clientMessageId: "send_1",
    });
    await vi.advanceTimersByTimeAsync(2 * PROBE_MS);
    stream.send(
      'event: assistant_message\ndata: {"id":"m2","content":"x"}\n\nevent: done\ndata: {}\n\n',
    );
    stream.close();
    await sent;
    expect(h.onAssistantMessage).toHaveBeenCalled();
    expect(streamBuffered()).toBe(false);
  });

  test("a stream that went quiet past the watchdog is a plain interruption", async () => {
    let stream!: ReturnType<typeof live>;
    fakeNetwork((signal) => {
      stream = live(signal);
      return Promise.resolve(stream.response);
    }, []);
    const sent = streamMessage("t1", "Hi", handlers()).catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(0);
    stream.send(`event: user_message\ndata: ${userMessage.data}\n\n`);
    // keepalives hold the watchdog off
    for (let i = 0; i < 4; i++) {
      await vi.advanceTimersByTimeAsync(HEARTBEAT_MS);
      stream.send(": keepalive\n\n");
    }
    await vi.advanceTimersByTimeAsync(WATCHDOG_MS);
    const error = await sent;
    expect(error).toBeInstanceOf(StreamInterruptedError);
    expect(error).not.toBeInstanceOf(StreamStalledError);
    expect(streamBuffered()).toBe(false);
  });

  test("a hidden page gives no verdict", async () => {
    hidden = true;
    const net = fakeNetwork(held, [() => pollAnswer()]);
    const controller = new AbortController();
    const sent = streamMessage("t1", "Hi", handlers(), controller.signal, {
      clientMessageId: "send_1",
    }).catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(3 * PROBE_MS);
    expect(net.pollURLs).toEqual([]);
    // the user's stop stays a stop
    controller.abort();
    const error = await sent;
    expect(error).toBeInstanceOf(DOMException);
    expect((error as DOMException).name).toBe("AbortError");
  });

  test("a page just back gives no verdict either", async () => {
    const net = fakeNetwork(held, [() => pollAnswer()]);
    const controller = new AbortController();
    const sent = streamMessage("t1", "Hi", handlers(), controller.signal, {
      clientMessageId: "send_1",
    }).catch(() => {});
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(PROBE_MS);
    expect(net.pollURLs).toEqual([]);
    controller.abort();
    await sent;
  });

  test("a send without a client id is never proven, so never broken off", async () => {
    const net = fakeNetwork(held, [() => pollAnswer()]);
    const controller = new AbortController();
    const sent = streamMessage("t1", "Hi", handlers(), controller.signal).catch(
      (e: unknown) => e,
    );
    await vi.advanceTimersByTimeAsync(3 * PROBE_MS);
    expect(net.pollURLs).toEqual([]);
    controller.abort();
    expect(await sent).toMatchObject({ name: "AbortError" });
  });

  test("a registered turn with nothing in its log yet is no proof", async () => {
    const net = fakeNetwork(held, [
      () => pollAnswer({ next: 0, events: [] }),
      () => pollAnswer(),
    ]);
    const sent = streamMessage("t1", "Hi", handlers(), undefined, {
      clientMessageId: "send_1",
    }).catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(PROBE_MS);
    expect(streamBuffered()).toBe(false);
    await vi.advanceTimersByTimeAsync(PROBE_MS);
    expect(await sent).toBeInstanceOf(StreamStalledError);
    expect(net.pollURLs).toHaveLength(2);
  });

  test("a page that knows the proxy probes the next send sooner", async () => {
    fakeNetwork(held, [() => pollAnswer()]);
    const first = streamMessage("t1", "Hi", handlers(), undefined, {
      clientMessageId: "send_1",
    }).catch(() => {});
    await vi.advanceTimersByTimeAsync(PROBE_MS);
    await first;
    const net = fakeNetwork(held, [() => pollAnswer({ sendId: "send_2" })]);
    const second = streamMessage("t1", "Again", handlers(), undefined, {
      clientMessageId: "send_2",
    }).catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(PROBE_KNOWN_MS);
    expect(await second).toBeInstanceOf(StreamStalledError);
    expect(net.pollURLs).toHaveLength(1);
  });

  test("a probe in flight is dropped once the stream's bytes come", async () => {
    let stream!: ReturnType<typeof live>;
    let probe: AbortSignal | null | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string, init?: RequestInit) => {
        if (url.includes("messages:poll")) {
          probe = init?.signal;
          return held(init?.signal);
        }
        stream = live(init?.signal);
        return Promise.resolve(stream.response);
      }),
    );
    const sent = streamMessage("t1", "Hi", handlers(), undefined, {
      clientMessageId: "send_1",
    });
    await vi.advanceTimersByTimeAsync(PROBE_MS);
    expect(probe?.aborted).toBe(false);
    stream.send(`event: user_message\ndata: ${userMessage.data}\n\n`);
    await vi.advanceTimersByTimeAsync(0);
    expect(probe?.aborted).toBe(true);
    stream.send(
      'event: assistant_message\ndata: {"id":"m2","content":"x"}\n\nevent: done\ndata: {}\n\n',
    );
    stream.close();
    await sent;
  });

  test("a stream that goes quiet after the answer arrived ends without failure", async () => {
    let stream!: ReturnType<typeof live>;
    fakeNetwork((signal) => {
      stream = live(signal);
      return Promise.resolve(stream.response);
    }, []);
    const h = handlers();
    const sent = streamMessage("t1", "Hi", h);
    await vi.advanceTimersByTimeAsync(0);
    stream.send(
      'event: assistant_message\ndata: {"id":"m2","content":"x"}\n\n',
    );
    await vi.advanceTimersByTimeAsync(WATCHDOG_MS);
    await sent;
    expect(h.onAssistantMessage).toHaveBeenCalled();
  });
});

describe("attachStream behind a buffering proxy", () => {
  test("goes on by polling once the attach is proven held", async () => {
    const net = fakeNetwork(held, [
      () => pollAnswer(), // the proof
      () => pollAnswer({ next: 2, events: [userMessage, delta("Hel")] }),
      () => pollAnswer({ next: 3, events: [delta("lo")] }),
      () => pollAnswer({ next: 5, done: true, events: [answer, done] }),
    ]);
    const h = handlers();
    const attached = attachStream("t1", h);
    await vi.advanceTimersByTimeAsync(PROBE_MS);
    expect(await attached).toBe("attached");
    expect(h.onUserMessage).toHaveBeenCalledTimes(1);
    expect(h.onDelta.mock.calls.map(([d]) => d).join("")).toBe("Hello");
    expect(h.onAssistantMessage).toHaveBeenCalledTimes(1);
    expect(net.pollURLs).toEqual([
      "/api/threads/t1/messages:poll",
      "/api/threads/t1/messages:poll",
      "/api/threads/t1/messages:poll?hub=h1&after=2",
      "/api/threads/t1/messages:poll?hub=h1&after=3",
    ]);
  });

  test("polls straight away once the page knows, and reads the server's answers", async () => {
    // prove it once
    fakeNetwork(held, [() => pollAnswer()]);
    const first = streamMessage("t1", "Hi", handlers(), undefined, {
      clientMessageId: "send_1",
    }).catch(() => {});
    await vi.advanceTimersByTimeAsync(PROBE_MS);
    await first;

    const stream = vi.fn(held);
    fakeNetwork(stream, [() => new Response(null, { status: 204 })]);
    expect(await attachStream("t1", handlers())).toBe("finished");
    expect(stream).not.toHaveBeenCalled();

    fakeNetwork(stream, [
      () => Response.json({ error: "hub_gone" }, { status: 409 }),
    ]);
    await expect(attachStream("t1", handlers())).rejects.toBeInstanceOf(
      StreamInterruptedError,
    );

    // done without a terminal event: the turn was cut, not finished
    fakeNetwork(stream, [
      () => pollAnswer({ next: 1, done: true, events: [userMessage] }),
    ]);
    await expect(attachStream("t1", handlers())).rejects.toBeInstanceOf(
      StreamInterruptedError,
    );

    // a network failure is an interruption, the caller's abort stays one
    vi.stubGlobal(
      "fetch",
      vi.fn(() => Promise.reject(new TypeError("Load failed"))),
    );
    await expect(attachStream("t1", handlers())).rejects.toBeInstanceOf(
      StreamInterruptedError,
    );
    fakeNetwork(stream, []);
    const controller = new AbortController();
    const attached = attachStream("t1", handlers(), controller.signal);
    controller.abort();
    await expect(attached).rejects.toMatchObject({ name: "AbortError" });
  });
});
