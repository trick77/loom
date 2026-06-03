import { afterEach, expect, test, vi } from "vitest";
import {
  AuthExpiredError,
  deleteThread,
  downloadArtifact,
  getMcpStatus,
  listProjects,
  listThreads,
  streamMessage,
  updateThread,
} from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

test("listThreads builds query parameters", async () => {
  const fetchMock = vi.fn().mockResolvedValue(Response.json([]));
  vi.stubGlobal("fetch", fetchMock);

  await listThreads({ starred: true, limit: 10 });

  expect(fetchMock).toHaveBeenCalledWith("/api/threads?starred=true&limit=10");
});

test("updateThread patches the thread title", async () => {
  const updated = {
    id: "t1",
    title: "Renamed chat",
    starred: false,
    createdAt: "2026-05-30T00:00:00Z",
    updatedAt: "2026-05-30T00:00:01Z",
  };
  const fetchMock = vi.fn().mockResolvedValue(Response.json(updated));
  vi.stubGlobal("fetch", fetchMock);

  await expect(updateThread("t1", { title: "Renamed chat" })).resolves.toEqual(updated);
  expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1", {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title: "Renamed chat" }),
  });
});

test("deleteThread deletes a thread", async () => {
  const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", fetchMock);

  await expect(deleteThread("t1")).resolves.toBeUndefined();
  expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1", { method: "DELETE" });
});

test("getMcpStatus loads current server counts", async () => {
  const fetchMock = vi.fn().mockResolvedValue(Response.json({ active: 1, configured: 2 }));
  vi.stubGlobal("fetch", fetchMock);

  await expect(getMcpStatus()).resolves.toEqual({ active: 1, configured: 2 });
  expect(fetchMock).toHaveBeenCalledWith("/api/mcp/status");
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

test("streamMessage parses assistant reasoning deltas", async () => {
  const body = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: assistant_reasoning_delta\ndata: {"content":"I checked "}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_reasoning_delta\ndata: {"content":"first."}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_delta\ndata: {"content":"Answer."}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status: 200 })));
  const reasoning: string[] = [];
  const deltas: string[] = [];

  await streamMessage("t1", "Hi", {
    onUserMessage: () => undefined,
    onDelta: (delta) => deltas.push(delta),
    onReasoningDelta: (delta) => reasoning.push(delta),
    onAssistantMessage: () => undefined,
    onThread: () => undefined,
  });

  expect(reasoning.join("")).toBe("I checked first.");
  expect(deltas.join("")).toBe("Answer.");
});

test("streamMessage parses tool events", async () => {
  const body = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: tool_call\ndata: {"id":"call_1","name":"search__web","arguments":"{}"}\n\n'));
      controller.enqueue(encoder.encode('event: tool_result\ndata: {"id":"call_1","name":"search__web","content":"result"}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status: 200 })));
  const events: string[] = [];

  await streamMessage("t1", "Hi", {
    onUserMessage: () => undefined,
    onDelta: () => undefined,
    onAssistantMessage: () => undefined,
    onThread: () => undefined,
    onToolCall: (event) => events.push(`call:${event.name}`),
    onToolResult: (event) => events.push(`result:${event.content}`),
  });

  expect(events).toEqual(["call:search__web", "result:result"]);
});

test("streamMessage parses mcp_status events", async () => {
  const body = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: mcp_status\ndata: {"active":2,"configured":3}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status: 200 })));
  let received: { active: number; configured: number } | null = null;

  await streamMessage("t1", "Hi", {
    onUserMessage: () => undefined,
    onDelta: () => undefined,
    onAssistantMessage: () => undefined,
    onThread: () => undefined,
    onMcpStatus: (event) => {
      received = event;
    },
  });

  expect(received).toEqual({ active: 2, configured: 3 });
});

test("streamMessage dispatches artifact events", async () => {
  const body = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(
        encoder.encode(
          'event: artifact\ndata: {"id":"art_1","displayFilename":"notes.md","mimeType":"text/markdown; charset=utf-8","sizeBytes":7,"downloadUrl":"/api/artifacts/art_1/download"}\n\n',
        ),
      );
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status: 200 })));
  const onArtifact = vi.fn();

  await streamMessage("t1", "Hi", {
    onUserMessage: () => undefined,
    onDelta: () => undefined,
    onAssistantMessage: () => undefined,
    onThread: () => undefined,
    onArtifact,
  });

  expect(onArtifact).toHaveBeenCalledWith(expect.objectContaining({ displayFilename: "notes.md" }));
});

test("downloadArtifact fetches the artifact blob", async () => {
  const blob = new Blob(["hello"], { type: "text/plain" });
  // Mock the response shape downloadArtifact relies on directly. Constructing a
  // real Response from a Blob calls blob.stream(), which jsdom's Blob lacks on
  // Node 22 (the CI runtime), so the test must not depend on it.
  const fetchMock = vi.fn().mockResolvedValue({
    status: 200,
    ok: true,
    blob: () => Promise.resolve(blob),
  });
  vi.stubGlobal("fetch", fetchMock);

  await expect(downloadArtifact("/api/artifacts/art_1/download")).resolves.toBeInstanceOf(Blob);
  expect(fetchMock).toHaveBeenCalledWith("/api/artifacts/art_1/download");
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

test("streamMessage surfaces the server error message on failure", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ error: "llm is not configured" }, { status: 503 })),
  );

  await expect(
    streamMessage("t1", "Hi", {
      onUserMessage: () => undefined,
      onDelta: () => undefined,
      onAssistantMessage: () => undefined,
      onThread: () => undefined,
    }),
  ).rejects.toThrow("llm is not configured");
});
