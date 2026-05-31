import { afterEach, expect, test, vi } from "vitest";
import { listThreads, streamMessage } from "./api";

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
