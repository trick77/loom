import { expect, test, vi } from "vitest";

import type { RunState } from "./streamRuns";
import { createTurnHandlers, newTempID, type RunPatch } from "./turnHandlers";

function harness() {
  let run: RunState = {
    blocks: [],
    sources: [],
    toolPending: false,
  } as unknown as RunState;
  const patches: RunPatch[] = [];
  const patch = (next: RunPatch) => {
    patches.push(next);
    run = { ...run, ...(typeof next === "function" ? next(run) : next) };
  };
  const onAssistantMessage = vi.fn();
  const turn = createTurnHandlers({ patch, onAssistantMessage });
  return { turn, patches, onAssistantMessage, run: () => run };
}

test("a burst of deltas accumulates into one text block and one run patch per microtask", async () => {
  const { turn, patches, run } = harness();
  turn.handlers.onDelta("Hel");
  turn.handlers.onDelta("lo");
  expect(patches).toHaveLength(0);
  await Promise.resolve();
  expect(patches).toHaveLength(1);
  expect(run().blocks).toEqual([{ type: "text", content: "Hello" }]);
  expect(turn.liveBlocks()).toEqual(run().blocks);
});

test("a flush queued before the assistant message does not resurrect the blocks", async () => {
  const { turn, run } = harness();
  turn.handlers.onDelta("answer");
  turn.handlers.onAssistantMessage({
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: "answer",
    createdAt: "x",
  });
  await Promise.resolve();
  await Promise.resolve();
  expect(run().blocks).toEqual([]);
});

test("a tool call clears the pending flag and upserts its trace block", async () => {
  const { turn, run } = harness();
  turn.handlers.onToolPending?.();
  expect(run().toolPending).toBe(true);
  turn.handlers.onToolCall?.({
    id: "c1",
    name: "fetch__fetch",
    arguments: "{}",
  });
  expect(run().toolPending).toBe(false);
  await Promise.resolve();
  expect(run().blocks[0]?.type).toBe("trace");
});

test("the assistant message receives the live blocks and the run is reset", () => {
  const { turn, onAssistantMessage, run } = harness();
  turn.handlers.onDelta("answer");
  const message = {
    id: "m1",
    threadId: "t1",
    role: "assistant" as const,
    content: "answer",
    createdAt: "x",
  };
  turn.handlers.onAssistantMessage(message);
  expect(onAssistantMessage).toHaveBeenCalledWith(message, [
    { type: "text", content: "answer" },
  ]);
  expect(run().blocks).toEqual([]);
  expect(run().toolPending).toBe(false);
});

test("source snapshots replace only their own kind", () => {
  const { turn, run } = harness();
  turn.handlers.onKnowledgeSources?.([
    { index: 1, filename: "doc.pdf" } as never,
  ]);
  turn.handlers.onWebSources?.([{ index: 2, url: "https://a" } as never]);
  turn.handlers.onWebSources?.([{ index: 2, url: "https://b" } as never]);
  expect(run().sources.map((s) => s.url ?? s.filename)).toEqual([
    "doc.pdf",
    "https://b",
  ]);
});

test("temp ids carry their prefix and never repeat", () => {
  const a = newTempID("temp-user");
  const b = newTempID("temp-user");
  expect(a.startsWith("temp-user-")).toBe(true);
  expect(a).not.toBe(b);
});

// pacedHarness runs the handlers with the typewriter on and animation frames
// stepped by hand.
function pacedHarness() {
  let now = 0;
  let frames: ((now: number) => void)[] = [];
  const clock = {
    requestFrame: (callback: (now: number) => void) => {
      frames.push(callback);
      return frames.length;
    },
    cancelFrame: () => {
      frames = [];
    },
    now: () => now,
    after: () => () => {},
  };
  const frame = () => {
    now += 1000 / 60;
    const due = frames;
    frames = [];
    for (const callback of due) callback(now);
  };
  let run: RunState = {
    blocks: [],
    sources: [],
    toolPending: false,
  } as unknown as RunState;
  const patch = (next: RunPatch) => {
    run = { ...run, ...(typeof next === "function" ? next(run) : next) };
  };
  const onAssistantMessage = vi.fn();
  const onMessageCost = vi.fn();
  const turn = createTurnHandlers({
    patch,
    onAssistantMessage,
    onMessageCost,
    pace: true,
    clock,
  });
  const text = () =>
    turn
      .liveBlocks()
      .filter((block) => block.type === "text")
      .map((block) => (block as { content: string }).content)
      .join("");
  return {
    turn,
    frame,
    text,
    onAssistantMessage,
    onMessageCost,
    run: () => run,
  };
}

const LONG = Array.from({ length: 120 }, (_, i) => `word${i}`).join(" ");
const MESSAGE = {
  id: "m1",
  threadId: "t1",
  role: "assistant" as const,
  content: LONG,
  createdAt: "x",
};

test("paced: a paragraph delta is typed out over frames", () => {
  const { turn, frame, text } = pacedHarness();
  turn.handlers.onDelta(LONG);
  expect(text()).toBe("");
  frame();
  expect(text().length).toBeGreaterThan(0);
  expect(text().length).toBeLessThan(LONG.length);
});

test("paced: the assistant message and later events wait for the typing to end", async () => {
  const { turn, frame, text, onAssistantMessage, onMessageCost } =
    pacedHarness();
  turn.handlers.onDelta(LONG);
  turn.handlers.onAssistantMessage(MESSAGE);
  turn.handlers.onMessageCost?.({ messageId: "m1" } as never);
  let drained = false;
  void turn.drained().then(() => {
    drained = true;
  });
  frame();
  expect(onAssistantMessage).not.toHaveBeenCalled();
  expect(onMessageCost).not.toHaveBeenCalled();
  for (let i = 0; i < 120 && !onAssistantMessage.mock.calls.length; i += 1)
    frame();
  expect(onAssistantMessage).toHaveBeenCalledOnce();
  expect(onAssistantMessage.mock.calls[0][1]).toEqual([
    { type: "text", content: LONG },
  ]);
  expect(onMessageCost).toHaveBeenCalledOnce();
  await Promise.resolve();
  expect(drained).toBe(true);
  expect(text()).toBe(LONG);
});

test("paced: a tool call lands after all the text before it", () => {
  const { turn, text } = pacedHarness();
  turn.handlers.onDelta(LONG);
  turn.handlers.onToolCall?.({
    id: "c1",
    name: "fetch__fetch",
    arguments: "{}",
  });
  expect(text()).toBe(LONG);
  expect(turn.liveBlocks().map((block) => block.type)).toEqual([
    "text",
    "trace",
  ]);
});

test("paced: finish shows what arrived and commits a held message", () => {
  const { turn, text, onAssistantMessage } = pacedHarness();
  turn.handlers.onDelta(LONG);
  turn.handlers.onAssistantMessage(MESSAGE);
  turn.finish();
  expect(onAssistantMessage).toHaveBeenCalledOnce();
  expect(text()).toBe(LONG);
});

test("paced: drained resolves on abort and at once when nothing is held", async () => {
  const { turn } = pacedHarness();
  await turn.drained();
  turn.handlers.onDelta(LONG);
  turn.handlers.onAssistantMessage(MESSAGE);
  const controller = new AbortController();
  const pending = turn.drained(controller.signal);
  controller.abort();
  await pending;
  await turn.drained(controller.signal);
});

test("paced: with no backlog the assistant message commits at once", () => {
  const { turn, onAssistantMessage } = pacedHarness();
  turn.handlers.onAssistantMessage(MESSAGE);
  expect(onAssistantMessage).toHaveBeenCalledOnce();
});

test("unpaced: drained and finish are no-ops", async () => {
  const { turn } = harness();
  await turn.drained();
  turn.finish();
});
