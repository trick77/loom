import { describe, expect, test } from "vitest";

import type { Artifact, ContentBlock, Message } from "../api";
import {
  appendArtifactBlock,
  appendReasoningDeltaBlock,
  appendTextDelta,
  applyReasoningTitleBlock,
  blocksFromLegacyMessage,
  graftStreamedBlocks,
  messageBlocks,
  upsertToolCallBlock,
  upsertToolResultBlock,
} from "./contentBlocks";

const artifact: Artifact = {
  id: "art-1",
  displayFilename: "chart.png",
  mimeType: "image/png",
  sizeBytes: 1024,
  downloadUrl: "/api/artifacts/art-1/download",
};

function baseMessage(overrides: Partial<Message>): Message {
  return {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: "",
    createdAt: "2026-06-19T00:00:00Z",
    ...overrides,
  };
}

describe("streaming reducer", () => {
  test("interleaves reasoning, prose, tool and artifact into chronological blocks", () => {
    let blocks: ContentBlock[] = [];
    blocks = appendReasoningDeltaBlock(blocks, "let me check");
    blocks = appendTextDelta(blocks, "Looking that up.");
    blocks = upsertToolCallBlock(blocks, { id: "c1", name: "search__web", arguments: '{"query":"x"}' });
    blocks = upsertToolResultBlock(blocks, { id: "c1", name: "search__web", content: "found it" });
    blocks = appendArtifactBlock(blocks, artifact);
    blocks = appendTextDelta(blocks, "Here is the result.");

    expect(blocks.map((block) => block.type)).toEqual(["trace", "text", "trace", "artifact", "text"]);

    const firstTrace = blocks[0];
    const secondTrace = blocks[2];
    if (firstTrace.type !== "trace" || secondTrace.type !== "trace") throw new Error("expected trace blocks");
    expect(firstTrace.events).toHaveLength(1);
    expect(firstTrace.events[0]).toMatchObject({ type: "reasoning", content: "let me check" });
    expect(secondTrace.events[0]).toMatchObject({ type: "tool", id: "c1", status: "done" });

    expect(blocks[1]).toEqual({ type: "text", content: "Looking that up." });
    expect(blocks[4]).toEqual({ type: "text", content: "Here is the result." });
  });

  test("consecutive trace events merge into one block until prose breaks the run", () => {
    let blocks: ContentBlock[] = [];
    blocks = appendReasoningDeltaBlock(blocks, "think");
    blocks = upsertToolCallBlock(blocks, { id: "c1", name: "a", arguments: "{}" });
    blocks = upsertToolCallBlock(blocks, { id: "c2", name: "b", arguments: "{}" });

    expect(blocks).toHaveLength(1);
    const trace = blocks[0];
    if (trace.type !== "trace") throw new Error("expected trace block");
    expect(trace.events.map((event) => event.type)).toEqual(["reasoning", "tool", "tool"]);
  });

  test("a reasoning title stamps the matching earlier trace block, not the trailing one", () => {
    let blocks: ContentBlock[] = [];
    blocks = appendReasoningDeltaBlock(blocks, "deliberating");
    blocks = appendTextDelta(blocks, "Answer streaming…");
    // The title arrives after prose has begun, so the trailing block is text.
    blocks = applyReasoningTitleBlock(blocks, "reasoning-1", "Deliberated the answer");

    expect(blocks.map((block) => block.type)).toEqual(["trace", "text"]);
    const trace = blocks[0];
    if (trace.type !== "trace") throw new Error("expected trace block");
    expect(trace.events[0]).toMatchObject({ type: "reasoning", title: "Deliberated the answer" });
  });

  test("a late tool result updates the tool in an earlier trace block", () => {
    let blocks: ContentBlock[] = [];
    blocks = upsertToolCallBlock(blocks, { id: "c1", name: "search__web", arguments: "{}" });
    blocks = appendTextDelta(blocks, "Working on it…");
    blocks = upsertToolResultBlock(blocks, { id: "c1", name: "search__web", content: "done" });

    expect(blocks.map((block) => block.type)).toEqual(["trace", "text"]);
    const trace = blocks[0];
    if (trace.type !== "trace") throw new Error("expected trace block");
    expect(trace.events[0]).toMatchObject({ type: "tool", id: "c1", status: "done" });
  });
});

describe("blocksFromLegacyMessage", () => {
  test("synthesizes trace-then-text-then-artifacts in the prior on-screen order", () => {
    const message = baseMessage({
      content: "The answer.",
      activityTrace: [{ id: "reasoning-1", type: "reasoning", content: "thought", status: "done" }],
      artifacts: [artifact],
    });

    expect(blocksFromLegacyMessage(message).map((block) => block.type)).toEqual(["trace", "text", "artifact"]);
  });

  test("messageBlocks prefers persisted contentBlocks when present", () => {
    const persisted: ContentBlock[] = [{ type: "text", content: "persisted" }];
    const message = baseMessage({ content: "legacy", contentBlocks: persisted });
    expect(messageBlocks(message)).toEqual(persisted);
  });
});

describe("graftStreamedBlocks", () => {
  test("replaces the trailing partial text block with the authoritative final answer", () => {
    const streamed: ContentBlock[] = [
      { type: "trace", events: [{ id: "reasoning-1", type: "reasoning", content: "x", status: "running" }] },
      { type: "text", content: "Partial" },
    ];
    const message = baseMessage({ id: "m2", content: "Partial answer." });

    const grafted = graftStreamedBlocks(message, streamed);
    expect(grafted.contentBlocks?.map((block) => block.type)).toEqual(["trace", "text"]);
    expect(grafted.contentBlocks?.[1]).toEqual({ type: "text", content: "Partial answer." });
    // The running reasoning event settles to done.
    const trace = grafted.contentBlocks?.[0];
    if (trace?.type !== "trace") throw new Error("expected trace block");
    expect(trace.events[0]).toMatchObject({ status: "done" });
  });

  test("appends the final answer when the stream produced no text block", () => {
    const streamed: ContentBlock[] = [{ type: "artifact", artifact }];
    const message = baseMessage({ id: "m2", content: "Generated the image." });

    const grafted = graftStreamedBlocks(message, streamed);
    expect(grafted.contentBlocks?.map((block) => block.type)).toEqual(["artifact", "text"]);
  });

  test("leaves the message untouched when the backend already sent contentBlocks", () => {
    const persisted: ContentBlock[] = [{ type: "text", content: "backend" }];
    const message = baseMessage({ id: "m2", content: "ignored", contentBlocks: persisted });
    expect(graftStreamedBlocks(message, [{ type: "text", content: "streamed" }]).contentBlocks).toEqual(persisted);
  });
});
