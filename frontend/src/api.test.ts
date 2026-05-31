import { afterEach, expect, test, vi } from "vitest";
import { AuthExpiredError, listProjects, listThreads, streamMessage } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

test("listThreads builds query parameters", async () => {
  const fetchMock = vi.fn().mockResolvedValue(Response.json([]));
  vi.stubGlobal("fetch", fetchMock);

  await listThreads({ starred: true, limit: 10 });

  expect(fetchMock).toHaveBeenCalledWith("/api/threads?starred=true&limit=10");
});

test("streamMessage parses server-sent events", async () => {
  const body = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: assistant_delta\ndata: {"content":"Hel"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_delta\ndata: {"content":"lo"}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status: 200 })));
  const deltas: string[] = [];

  await streamMessage("t1", "Hi", {
    onUserMessage: () => undefined,
    onDelta: (delta) => deltas.push(delta),
    onAssistantMessage: () => undefined,
    onThread: () => undefined,
  });

  expect(deltas.join("")).toBe("Hello");
});

test("streamMessage throws server-sent error events", async () => {
  const body = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: error\ndata: {"error":"stream failed"}\n\n'));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status: 200 })));

  await expect(
    streamMessage("t1", "Hi", {
      onUserMessage: () => undefined,
      onDelta: () => undefined,
      onAssistantMessage: () => undefined,
      onThread: () => undefined,
    }),
  ).rejects.toThrow("stream failed");
});

test("api helpers throw AuthExpiredError on 401", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("", { status: 401 })));

  await expect(listProjects()).rejects.toBeInstanceOf(AuthExpiredError);
});

test("streamMessage passes abort signal to fetch", async () => {
  const controller = new AbortController();
  const fetchMock = vi.fn().mockResolvedValue(Response.json({}, { status: 503 }));
  vi.stubGlobal("fetch", fetchMock);

  await expect(
    streamMessage(
      "t1",
      "Hi",
      {
        onUserMessage: () => undefined,
        onDelta: () => undefined,
        onAssistantMessage: () => undefined,
        onThread: () => undefined,
      },
      controller.signal,
    ),
  ).rejects.toThrow("failed to stream message");

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads/t1/messages:stream",
    expect.objectContaining({ signal: controller.signal }),
  );
});
