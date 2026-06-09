import { describe, expect, test } from "vitest";
import {
  appendReasoningDelta,
  completeTrace,
  normalizeActivityTrace,
  summarizeToolCall,
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
        summary: { kind: "search", title: "agentgateway" },
        rawArguments: "{}",
      },
    ]);

    expect(events.every((event) => event.status === "done")).toBe(true);
  });

  test("shows the reasoning abstract, not tool stats, when a trace has tools", () => {
    const summary = summarizeTrace([
      {
        id: "reasoning-1",
        type: "reasoning",
        content: "I should compare the two proxies before answering.",
        status: "done",
      },
      {
        id: "call_1",
        type: "tool",
        name: "search__web",
        status: "done",
        summary: { kind: "search", title: "agentgateway" },
        preview: { kind: "searchResults", resultCount: 2, results: [] },
        rawArguments: "{}",
      },
      {
        id: "call_2",
        type: "tool",
        name: "fetch__fetch",
        status: "failed",
        summary: { kind: "fetch", title: "example.com" },
        rawArguments: "{}",
        rawOutput: "tool failed: timeout",
      },
    ]);

    expect(summary).toBe("Compared the two proxies");
  });

  test("falls back to a neutral label when a completed trace has tools but no reasoning", () => {
    const summary = summarizeTrace([
      {
        id: "call_1",
        type: "tool",
        name: "custom__lookup",
        status: "failed",
        summary: { kind: "generic", title: "custom lookup" },
        rawArguments: "{}",
        rawOutput: "tool failed: timeout",
      },
    ]);

    expect(summary).toBe("Activity complete");
  });

  test("summarizes reasoning-only traces from cached reasoning content", () => {
    const summary = summarizeTrace([
      {
        id: "reasoning-1",
        type: "reasoning",
        content: "I should compare the active stream state with the completed assistant message before answering.",
        status: "done",
      },
    ]);

    expect(summary).toBe("Compared the active stream state with the completed assistant message");
  });

  test("keeps the fetched URL on the summary and renders no result frame", () => {
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
      summary: { kind: "fetch", title: "example.com", url: "https://example.com/docs" },
      preview: { kind: "text", detail: "Example documentation page content" },
    });
  });

  test("normalizes persisted generic tool traces from raw call data", () => {
    const events = normalizeActivityTrace([
      {
        id: "call_future",
        type: "tool",
        name: "acme__transmogrify_asset",
        status: "running",
        rawArguments: "{\"asset\":\"draft.pdf\"}",
        rawOutput: "Created draft.pdf",
      },
    ] as ActivityTraceEvent[]);

    expect(events?.[0]).toMatchObject({
      id: "call_future",
      type: "tool",
      name: "acme__transmogrify_asset",
      status: "done",
      summary: {
        kind: "generic",
        title: "acme transmogrify asset",
      },
      preview: {
        kind: "text",
        detail: "Created draft.pdf",
      },
      rawOutput: "Created draft.pdf",
    });
  });

  test("derives tool titles from query, url, filename and readable name", () => {
    expect(summarizeToolCall("tavily__tavily_search", "{\"query\":\"balcony glazing\"}")).toMatchObject({
      title: "balcony glazing",
    });
    expect(summarizeToolCall("fetch__fetch", "{\"url\":\"https://example.com\"}")).toMatchObject({
      title: "example.com",
    });
    expect(summarizeToolCall("generate_image", "{\"prompt\":\"a cabin\"}")).toMatchObject({
      title: "generate image",
    });
    expect(summarizeToolCall("create_pptx_presentation", "{\"filename\":\"deck.pptx\"}")).toMatchObject({
      title: "deck.pptx",
    });
    expect(summarizeToolCall("custom__lookup", "not-json")).toMatchObject({
      title: "custom lookup",
    });
  });

  test("preserves URL fallback text for search results without titles", () => {
    let events: ActivityTraceEvent[] = [];

    events = upsertTraceToolCall(events, {
      id: "call_1",
      name: "tavily__tavily_search",
      arguments: "{\"query\":\"example\"}",
    });
    events = upsertTraceToolResult(events, {
      id: "call_1",
      name: "tavily__tavily_search",
      content: "{\"results\":[{\"url\":\"https://example.com/my_page\"}]}",
    });

    expect(events[0]).toMatchObject({
      preview: {
        kind: "searchResults",
        results: [
          {
            title: "https://example.com/my_page",
            url: "https://example.com/my_page",
          },
        ],
      },
    });
  });
});
