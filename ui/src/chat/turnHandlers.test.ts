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

test("deltas accumulate into text blocks and each one patches the run", () => {
  const { turn, patches, run } = harness();
  turn.handlers.onDelta("Hel");
  turn.handlers.onDelta("lo");
  expect(patches).toHaveLength(2);
  expect(run().blocks).toEqual([{ type: "text", content: "Hello" }]);
  expect(turn.liveBlocks()).toEqual(run().blocks);
});

test("a tool call clears the pending flag and upserts its trace block", () => {
  const { turn, run } = harness();
  turn.handlers.onToolPending?.();
  expect(run().toolPending).toBe(true);
  turn.handlers.onToolCall?.({
    id: "c1",
    name: "fetch__fetch",
    arguments: "{}",
  });
  expect(run().toolPending).toBe(false);
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
