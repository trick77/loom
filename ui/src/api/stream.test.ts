import { describe, expect, test, vi } from "vitest";

import {
  PayloadTooLargeError,
  StreamFailedError,
  StreamInterruptedError,
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
