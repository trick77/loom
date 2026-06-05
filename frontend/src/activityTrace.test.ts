import { describe, expect, test } from "vitest";
import {
  appendReasoningDelta,
  completeTrace,
  summarizeTrace,
  upsertTraceToolCall,
  upsertTraceToolResult,
  type ActivityTraceEvent,
} from "./activityTrace";

describe("activity trace model", () => {
  test("groups adjacent reasoning deltas into one running reasoning event", () => {
    let events: ActivityTraceEvent[] = [];

    events = appendReasoningDelta(events, "I should search ");
    events = appendReasoningDelta(events, "current sources.");

    expect(events).toEqual([
      {
        id: "reasoning-1",
        type: "reasoning",
        content: "I should search current sources.",
        status: "running",
      },
    ]);
  });

  test("starts a new reasoning block after a tool event", () => {
    let events: ActivityTraceEvent[] = [];

    events = appendReasoningDelta(events, "First thought.");
    events = upsertTraceToolCall(events, {
      id: "call_1",
      name: "search__web",
      arguments: "{\"query\":\"agentgateway kgateway\"}",
    });
    events = appendReasoningDelta(events, "Next thought.");

    expect(events.map((event) => event.type)).toEqual(["reasoning", "tool", "reasoning"]);
    expect(events[2]).toMatchObject({
      id: "reasoning-2",
      type: "reasoning",
      content: "Next thought.",
    });
  });

  test("marks all running trace events done on completion", () => {
    const events = completeTrace([
      {
        id: "reasoning-1",
        type: "reasoning",
        content: "Checking.",
        status: "running",
      },
      {
        id: "call_1",
        type: "tool",
        name: "search__web",
        status: "running",
        summary: { kind: "search", title: "agentgateway", detail: "search__web" },
        rawArguments: "{}",
      },
    ]);

    expect(events.every((event) => event.status === "done")).toBe(true);
  });

  test("summarizes completed search and failed tool activity", () => {
    const summary = summarizeTrace([
      {
        id: "call_1",
        type: "tool",
        name: "search__web",
        status: "done",
        summary: { kind: "search", title: "agentgateway", detail: "search__web" },
        preview: { kind: "searchResults", resultCount: 2, results: [] },
        rawArguments: "{}",
      },
      {
        id: "call_2",
        type: "tool",
        name: "fetch__fetch",
        status: "failed",
        summary: { kind: "fetch", title: "example.com", detail: "https://example.com" },
        rawArguments: "{}",
        rawOutput: "tool failed: timeout",
      },
    ]);

    expect(summary).toBe("Searched 1 query · read 1 page · 1 tool failed");
  });

  test("does not double-count a failed generic tool as used and failed", () => {
    const summary = summarizeTrace([
      {
        id: "call_1",
        type: "tool",
        name: "custom__lookup",
        status: "failed",
        summary: { kind: "generic", title: "custom lookup", detail: "custom lookup" },
        rawArguments: "{}",
        rawOutput: "tool failed: timeout",
      },
    ]);

    expect(summary).toBe("1 tool failed");
  });

  test("creates a fetch result preview for fetch-like tools", () => {
    let events: ActivityTraceEvent[] = [];
    events = upsertTraceToolCall(events, {
      id: "call_1",
      name: "fetch__fetch",
      arguments: "{\"url\":\"https://example.com/docs\"}",
    });
    events = upsertTraceToolResult(events, {
      id: "call_1",
      name: "fetch__fetch",
      content: "Example documentation page content",
    });

    expect(events[0]).toMatchObject({
      preview: {
        kind: "fetchResult",
        domain: "example.com",
        detail: "Example documentation page content",
      },
    });
  });
});
