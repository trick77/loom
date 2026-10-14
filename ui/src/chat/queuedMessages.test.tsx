import "@testing-library/jest-dom/vitest";
import {
  act,
  fireEvent,
  render,
  renderHook,
  screen,
} from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { resetModelInfoForTest } from "../api/model";
import { streamMessage } from "../api/stream";
import { Composer } from "./Composer";
import { restoreQueuedDrafts, threadDraftScope } from "./composerDrafts";
import type { RunState } from "./streamRuns";
import { createTurnHandlers, type RunPatch } from "./turnHandlers";
import { useQueuedMessages } from "./useQueuedMessages";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  resetModelInfoForTest();
});

function composer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  const onSend = vi.fn();
  const onStop = vi.fn();
  render(
    <Composer
      variant="thread"
      draft="and then?"
      isSending
      canQueue
      placeholder="Message"
      onDraftChange={() => undefined}
      onSend={onSend}
      onStop={onStop}
      {...props}
    />,
  );
  return { onSend, onStop };
}

test("while the answer streams, a send queues and stop is its own button", () => {
  const { onSend, onStop } = composer();

  fireEvent.click(screen.getByRole("button", { name: "Queue message" }));
  expect(onSend).toHaveBeenCalledTimes(1);
  expect(onStop).not.toHaveBeenCalled();

  fireEvent.keyDown(screen.getByPlaceholderText("Message"), { key: "Enter" });
  expect(onSend).toHaveBeenCalledTimes(2);

  fireEvent.click(screen.getByRole("button", { name: "Stop response" }));
  expect(onStop).toHaveBeenCalledWith();
});

test("staged attachments hold a queued send back", () => {
  const { onSend } = composer({
    attachments: [
      {
        id: "a1",
        filename: "notes.txt",
        mimeType: "text/plain",
        sizeBytes: 3,
        status: "ready",
      },
    ],
  });
  expect(screen.getByRole("button", { name: "Queue message" })).toBeDisabled();
  fireEvent.keyDown(screen.getByPlaceholderText("Message"), { key: "Enter" });
  expect(onSend).not.toHaveBeenCalled();
});

test("without queueing the submit button stays the stop button", () => {
  const { onSend, onStop } = composer({ canQueue: false });
  fireEvent.click(screen.getByRole("button", { name: "Stop response" }));
  expect(onStop).toHaveBeenCalled();
  expect(onSend).not.toHaveBeenCalled();
});

function harness() {
  let run = {
    blocks: [],
    sources: [],
    toolPending: false,
    error: "",
  } as unknown as RunState;
  const patch = (next: RunPatch) => {
    run = { ...run, ...(typeof next === "function" ? next(run) : next) };
  };
  const onAssistantMessage = vi.fn();
  const turn = createTurnHandlers({ patch, onAssistantMessage });
  return { turn, onAssistantMessage, run: () => run };
}

const message = (id: string, content: string) => ({
  id,
  threadId: "t1",
  role: "assistant" as const,
  content,
  createdAt: "x",
});

test("a segment settles the answer so far and the turn goes on from nothing", async () => {
  const { turn, onAssistantMessage, run } = harness();
  turn.handlers.onDelta("part one");
  turn.handlers.onAssistantSegment?.(message("seg", "part one"));
  expect(onAssistantMessage).toHaveBeenCalledWith(message("seg", "part one"), [
    { type: "text", content: "part one" },
  ]);
  await Promise.resolve();
  expect(run().blocks).toEqual([]);

  turn.handlers.onDelta("part two");
  turn.handlers.onAssistantMessage(message("final", "part two"));
  expect(onAssistantMessage).toHaveBeenLastCalledWith(
    message("final", "part two"),
    [{ type: "text", content: "part two" }],
  );
});

test("a follow-up turn on the same stream does not carry the previous blocks", () => {
  const { turn, onAssistantMessage } = harness();
  turn.handlers.onDelta("first");
  turn.handlers.onAssistantMessage(message("a1", "first"));
  turn.handlers.onDelta("second");
  turn.handlers.onAssistantMessage(message("a2", "second"));
  expect(onAssistantMessage).toHaveBeenLastCalledWith(message("a2", "second"), [
    { type: "text", content: "second" },
  ]);
});

test("a failed turn followed by another reports its error and clears its blocks", async () => {
  const { turn, run } = harness();
  turn.handlers.onDelta("partial");
  turn.handlers.onTurnError?.("upstream down");
  await Promise.resolve();
  expect(run().error).toBe("upstream down");
  expect(run().blocks).toEqual([]);
});

test("segments and turn errors do not end the stream", async () => {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(
        encoder.encode(
          'event: assistant_segment\ndata: {"id":"s1"}\n\n' +
            'event: turn_error\ndata: {"error":"boom"}\n\n' +
            'event: user_message\ndata: {"id":"u2","clientMessageId":"send-2"}\n\n' +
            'event: assistant_message\ndata: {"id":"a2"}\n\n' +
            "event: done\ndata: {}\n\n",
        ),
      );
      controller.close();
    },
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(body, { status: 200 })),
  );
  const handlers = {
    onUserMessage: vi.fn(),
    onDelta: vi.fn(),
    onAssistantMessage: vi.fn(),
    onAssistantSegment: vi.fn(),
    onTurnError: vi.fn(),
    onThread: vi.fn(),
  };
  await streamMessage("t1", "Hi", handlers);
  expect(handlers.onAssistantSegment).toHaveBeenCalledWith({ id: "s1" });
  expect(handlers.onTurnError).toHaveBeenCalledWith("boom");
  expect(handlers.onUserMessage).toHaveBeenCalledWith({
    id: "u2",
    clientMessageId: "send-2",
  });
  expect(handlers.onAssistantMessage).toHaveBeenCalledWith({ id: "a2" });
});

test("handed-back messages go after the draft, pastes as chips", () => {
  const scope = threadDraftScope("t1");
  const paste = { text: "x".repeat(10), lineCount: 1 };
  const drafts = restoreQueuedDrafts(
    { [scope]: { text: "typing", pastedTexts: [] } },
    scope,
    [
      { content: "first", pastedTexts: [] },
      { content: `second\n\n${paste.text}`, pastedTexts: [paste] },
    ],
  );
  expect(drafts[scope].text).toBe("typing\n\nfirst\n\nsecond");
  expect(drafts[scope].pastedTexts.map((p) => p.text)).toEqual([paste.text]);
});

test("the queue is kept per thread", () => {
  const { result } = renderHook(() => useQueuedMessages());
  const bubble = { clientMessageId: "send-1", content: "a", pastedTexts: [] };
  act(() => result.current.add("t1", bubble));
  expect(result.current.has("t1")).toBe(true);
  expect(result.current.has("t2")).toBe(false);
  let removed = false;
  act(() => {
    removed = result.current.remove("t1", "send-1");
  });
  expect(removed).toBe(true);
  expect(result.current.queued).toEqual({});
  act(() =>
    result.current.replace("t2", [
      {
        id: "q",
        threadId: "t2",
        content: "b",
        pastedTexts: [],
        clientMessageId: "send-9",
        createdAt: "x",
      },
    ]),
  );
  expect(result.current.queued.t2).toEqual([
    { clientMessageId: "send-9", content: "b", pastedTexts: [] },
  ]);
});
