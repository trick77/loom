import { describe, expect, test } from "vitest";
import {
  normalizeActivityTrace,
  summarizeToolCall,
  toolSourceLabel,
  upsertTraceToolCall,
} from "./activityTrace";

describe("tool call descriptions and labels", () => {
  test("a server-given title says what the call does, with its query", () => {
    expect(
      summarizeToolCall(
        "peeq__search",
        '{"query":"bike stability"}',
        "Search your video library",
      ),
    ).toEqual({
      kind: "lookup",
      title: "Search your video library: bike stability",
    });
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
