import { describe, expect, test } from "vitest";
import {
  completeTrace,
  normalizeActivityTrace,
  summarizeToolCall,
  toolSourceLabel,
  upsertTraceToolCall,
  upsertTraceToolResult,
} from "./activityTrace";

describe("tool call descriptions and labels", () => {
  test("a titled call without arguments yet never shows the tool's name", () => {
    // A call streams in before its arguments: the server's title ("Tavily
    // Search") would name the vendor, so the row says which source is in use
    // and hides the tag that would repeat it.
    expect(
      summarizeToolCall("tavily__tavily_search", "{}", "Tavily Search", {
        running: true,
      }),
    ).toEqual({
      kind: "generic",
      title: "Using web search",
      placeholder: true,
    });
    expect(
      summarizeToolCall("context7__query-docs", "", "Query Documentation", {
        running: true,
      }),
    ).toEqual({ kind: "generic", title: "Using context7", placeholder: true });
    expect(
      summarizeToolCall("peeq__get_video", "{}", "Get Video", {
        running: true,
      }),
    ).toEqual({ kind: "generic", title: "Using peeq", placeholder: true });
  });

  test("the placeholder gives way to the query or, at the result, the title", () => {
    let events = upsertTraceToolCall([], {
      id: "c1",
      name: "tavily__tavily_search",
      arguments: "",
      title: "Tavily Search",
    });
    expect(events[0]).toMatchObject({ summary: { title: "Using web search" } });
    events = upsertTraceToolCall(events, {
      id: "c1",
      name: "tavily__tavily_search",
      arguments: '{"query":"swiss rail"}',
      title: "Tavily Search",
    });
    expect(events[0]).toMatchObject({ summary: { title: "Swiss rail" } });

    // A tool without arguments keeps "{}": its title shows once it returns.
    events = upsertTraceToolCall([], {
      id: "c2",
      name: "peeq__list_youtube_channels",
      arguments: "{}",
      title: "List channels",
    });
    expect(events[0]).toMatchObject({ summary: { title: "Using peeq" } });
    events = upsertTraceToolResult(events, {
      id: "c2",
      name: "peeq__list_youtube_channels",
      content: "[]",
    });
    expect(events[0]).toMatchObject({
      summary: { kind: "generic", title: "List channels" },
    });
    expect(events[0]).not.toHaveProperty("summary.placeholder");
  });

  test("malformed arguments are no placeholder", () => {
    expect(
      summarizeToolCall("peeq__get_video", "[1]", "Get Video", {
        running: true,
      }),
    ).toEqual({ kind: "generic", title: "Get Video" });
  });

  test("a call stopped before its result drops the placeholder", () => {
    const running = upsertTraceToolCall([], {
      id: "c1",
      name: "tavily__tavily_search",
      arguments: "",
      title: "Tavily Search",
    });
    expect(completeTrace(running)[0]).toMatchObject({
      status: "done",
      summary: { kind: "generic", title: "Tavily Search" },
    });
    expect(completeTrace(running)[0]).not.toHaveProperty("summary.placeholder");
    const reloaded = normalizeActivityTrace([
      { ...running[0], status: "done" },
    ] as never);
    expect(reloaded?.[0]).not.toHaveProperty("summary.placeholder");
  });

  test("a query stands alone; the title only speaks when there is none", () => {
    // The step's tag already names the tool ("Web search", the server), so a
    // title in front of the query ("Tavily Search: …") only repeats it.
    expect(
      summarizeToolCall(
        "peeq__search",
        '{"query":"bike stability"}',
        "Search your video library",
      ),
    ).toEqual({ kind: "lookup", title: "Bike stability" });
    expect(
      summarizeToolCall(
        "tavily__tavily_search",
        '{"query":"UBS move headquarters"}',
        "Tavily Search",
      ).title,
    ).toBe("UBS move headquarters");
    // No query: an id or other argument is not worth showing.
    expect(
      summarizeToolCall(
        "peeq__get_video",
        '{"id":"abc"}',
        "Get a video's summaries",
      ),
    ).toEqual({ kind: "generic", title: "Get a video's summaries" });
    expect(
      summarizeToolCall("peeq__list_youtube_channels", "{}", "List channels"),
    ).toEqual({ kind: "generic", title: "List channels" });
  });

  test("the query is tidied and capped; other arguments never show", () => {
    expect(
      summarizeToolCall(
        "peeq__search",
        JSON.stringify({ channel: "UCbikes", query: "  bike\n  stability " }),
        "Search your video library",
      ).title,
    ).toBe("Bike stability");
    const long = "x".repeat(200);
    expect(
      summarizeToolCall(
        "peeq__search",
        JSON.stringify({ query: long }),
        "Search",
      ).title,
    ).toBe(`X${"x".repeat(79)}...`);
  });

  test("a titled page fetch keeps its domain, favicon and link", () => {
    expect(
      summarizeToolCall(
        "acme__open_page",
        '{"url":"https://example.com/a"}',
        "Open a page",
      ),
    ).toMatchObject({ kind: "fetch", url: "https://example.com/a" });
  });

  test("a titled IP lookup keeps its address", () => {
    expect(
      summarizeToolCall("ipverse__whois", '{"ip":"1.2.3.4"}', "IP lookup"),
    ).toMatchObject({ kind: "lookup", title: "Looking up 1.2.3.4" });
  });

  test("the live tool_call title is kept and survives a reload", () => {
    const live = upsertTraceToolCall([], {
      id: "c1",
      name: "peeq__get_video",
      arguments: '{"id":"abc"}',
      title: "Get a video's summaries",
    });
    expect(live[0]).toMatchObject({
      toolTitle: "Get a video's summaries",
      summary: { title: "Get a video's summaries" },
    });
    const reloaded = normalizeActivityTrace([
      {
        id: "c1",
        type: "tool",
        name: "peeq__get_video",
        status: "done",
        rawArguments: '{"id":"abc"}',
        toolTitle: "Get a video's summaries",
      },
    ] as never);
    expect(reloaded?.[0]).toMatchObject({
      summary: { title: "Get a video's summaries" },
    });
  });

  test("an untitled tool reads as its own name, without the server prefix", () => {
    expect(summarizeToolCall("peeq__get_video", "{}").title).toBe("Get video");
    expect(summarizeToolCall("acme__resolve-item-id", "{}").title).toBe(
      "Resolve item id",
    );
  });

  test("memory tools say what they do", () => {
    expect(summarizeToolCall("remember_user_directive", "{}").title).toBe(
      "Saving a memory",
    );
  });

  test("every tool names its source", () => {
    expect(toolSourceLabel("peeq__search")).toBe("peeq");
    expect(toolSourceLabel("context7__query-docs")).toBe("context7");
    expect(toolSourceLabel("tavily__tavily_search")).toBe("Web search");
    expect(toolSourceLabel("fetch__fetch")).toBe("Web fetch");
    expect(toolSourceLabel("obscura__browser_navigate")).toBe("Browser");
    expect(toolSourceLabel("run_python")).toBe("Python sandbox");
    expect(toolSourceLabel("conversation_search")).toBe("Your conversations");
    expect(toolSourceLabel("create_pdf_file")).toBe("File generator");
    expect(toolSourceLabel("generate_image")).toBe("Image generator");
    expect(toolSourceLabel("forget_user_directive")).toBe("Memory");
    expect(toolSourceLabel("something_new")).toBe("loom");
  });
});
