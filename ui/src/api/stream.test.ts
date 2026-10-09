import { describe, expect, test, vi } from "vitest";

import {
  PayloadTooLargeError,
  StreamFailedError,
  StreamInterruptedError,
  attachStream,
  streamIncognitoMessage,
  streamMessage,
} from "./stream";

function sseBody(chunks: string[], close = true) {
  const encoder = new TextEncoder();
  return new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      if (close) controller.close();
    },
  });
}

// droppedBody delivers chunks, then fails the read the way a browser does when
// the network goes away (Safari: TypeError "Load failed").
function droppedBody(chunks: string[]) {
  const encoder = new TextEncoder();
  const pending = [...chunks];
  return new ReadableStream<Uint8Array>({
    // Pulled one chunk at a time: erroring a stream drops what is queued.
    pull(controller) {
      const chunk = pending.shift();
      if (chunk === undefined) controller.error(new TypeError("Load failed"));
      else controller.enqueue(encoder.encode(chunk));
    },
  });
}

function handlers() {
  return {
    onUserMessage: vi.fn(),
    onDelta: vi.fn(),
    onAssistantMessage: vi.fn(),
    onThread: vi.fn(),
  };
}

describe("streamMessage", () => {
  test("delivers message_cost, which the server sends after assistant_message", async () => {
    const body = sseBody([
      'event: assistant_message\ndata: {"id":"m2","content":"x"}\n\n',
      'event: message_cost\ndata: {"id":"m2","costNanoUsd":1110}\n\n',
      "event: done\ndata: {}\n\n",
    ]);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));
    const h = { ...handlers(), onMessageCost: vi.fn() };

    await streamMessage("t1", "hi", h);

    expect(h.onMessageCost).toHaveBeenCalledWith({
      id: "m2",
      costNanoUsd: 1110,
    });
  });

  test("delivers the working title, the turn's first sweep line", async () => {
    const body = sseBody([
      'event: assistant_working_title\ndata: {"title":"Checking whether 1001 is prime"}\n\n',
      'event: assistant_message\ndata: {"id":"m2","content":"x"}\n\n',
      "event: done\ndata: {}\n\n",
    ]);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));
    const h = { ...handlers(), onWorkingTitle: vi.fn() };

    await streamMessage("t1", "hi", h);

    expect(h.onWorkingTitle).toHaveBeenCalledWith(
      "Checking whether 1001 is prime",
    );
  });

  test("rejects with StreamInterruptedError when the body closes before a terminal event", async () => {
    const body = sseBody([
      'event: user_message\ndata: {"id":"m1"}\n\n',
      'event: assistant_delta\ndata: {"content":"partial"}\n\n',
    ]);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));
    const h = handlers();

    await expect(streamMessage("t1", "hi", h)).rejects.toBeInstanceOf(
      StreamInterruptedError,
    );
    // What streamed before the cut was still delivered.
    expect(h.onDelta).toHaveBeenCalledWith("partial");
    expect(h.onAssistantMessage).not.toHaveBeenCalled();
  });

  test("resolves once the assistant message and done have arrived", async () => {
    const body = sseBody([
      'event: assistant_message\ndata: {"id":"m2","content":"x"}\n\n',
      "event: done\ndata: {}\n\n",
    ]);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));
    const h = handlers();

    await expect(streamMessage("t1", "hi", h)).resolves.toBeUndefined();
    expect(h.onAssistantMessage).toHaveBeenCalledWith({
      id: "m2",
      content: "x",
    });
  });

  test("accepts CRLF event separators, including one split across chunks", async () => {
    const body = sseBody([
      'event: assistant_delta\r\ndata: {"content":"a"}\r',
      "\n\r\nevent: done\r\ndata: {}\r\n\r\n",
    ]);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));
    const h = handlers();

    await expect(streamMessage("t1", "hi", h)).resolves.toBeUndefined();
    expect(h.onDelta).toHaveBeenCalledWith("a");
  });

  test("surfaces the server's error event and cancels the body", async () => {
    let cancelled = false;
    const encoder = new TextEncoder();
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(
          encoder.encode(
            'event: error\ndata: {"error":"image generation was not completed"}\n\n',
          ),
        );
      },
      cancel() {
        cancelled = true;
      },
    });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));

    await expect(streamMessage("t1", "hi", handlers())).rejects.toThrow(
      new StreamFailedError("image generation was not completed"),
    );
    expect(cancelled).toBe(true);
  });

  test("maps a 413 to PayloadTooLargeError before touching the body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("", { status: 413 })),
    );

    await expect(streamMessage("t1", "hi", handlers())).rejects.toBeInstanceOf(
      PayloadTooLargeError,
    );
  });

  test("reports a pre-stream 400 with the server's message", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: "too many image attachments" }), {
          status: 400,
        }),
      ),
    );

    await expect(streamMessage("t1", "hi", handlers())).rejects.toThrow(
      "too many image attachments",
    );
  });
});

test("streamIncognitoMessage shares the interruption rule", async () => {
  const body = sseBody(['event: assistant_delta\ndata: {"content":"p"}\n\n']);
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));

  await expect(
    streamIncognitoMessage("hi", [], handlers()),
  ).rejects.toBeInstanceOf(StreamInterruptedError);
});

describe("a dropped connection", () => {
  test("a read that fails mid-turn is an interruption, not a send failure", async () => {
    const body = droppedBody([
      'event: user_message\ndata: {"id":"m1"}\n\n',
      'event: assistant_delta\ndata: {"content":"partial"}\n\n',
    ]);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));
    const h = handlers();

    await expect(streamMessage("t1", "hi", h)).rejects.toBeInstanceOf(
      StreamInterruptedError,
    );
    expect(h.onDelta).toHaveBeenCalledWith("partial");
  });

  test("a read that fails after the answer arrived is no failure", async () => {
    const body = droppedBody([
      'event: assistant_message\ndata: {"id":"m2","content":"x"}\n\n',
    ]);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));

    await expect(
      streamMessage("t1", "hi", handlers()),
    ).resolves.toBeUndefined();
  });

  test("our own abort stays an AbortError", async () => {
    const abort = new DOMException("aborted", "AbortError");
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.error(abort);
      },
    });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body)));

    await expect(streamMessage("t1", "hi", handlers())).rejects.toBe(abort);
  });
});

describe("attachStream", () => {
  test("follows the running turn from its first event", async () => {
    const body = sseBody([
      'event: user_message\ndata: {"id":"m1"}\n\n',
      'event: assistant_delta\ndata: {"content":"Hello"}\n\n',
      'event: assistant_message\ndata: {"id":"m2","content":"Hello"}\n\n',
      "event: done\ndata: {}\n\n",
    ]);
    const fetchMock = vi.fn().mockResolvedValue(new Response(body));
    vi.stubGlobal("fetch", fetchMock);
    const h = handlers();

    await expect(attachStream("t 1", h)).resolves.toBe("attached");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t%201/messages:attach",
      expect.objectContaining({ method: "GET" }),
    );
    expect(h.onDelta).toHaveBeenCalledWith("Hello");
    expect(h.onAssistantMessage).toHaveBeenCalled();
  });

  test("reports a turn that already finished", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 204 })),
    );

    await expect(attachStream("t1", handlers())).resolves.toBe("finished");
  });
});
