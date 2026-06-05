import type { ToolCallEvent, ToolResultEvent } from "./api";

export type ActivityTraceEvent =
  | {
      id: string;
      type: "reasoning";
      content: string;
      status: "running" | "done";
    }
  | ActivityTraceToolEvent;

export type ActivityTraceToolEvent = {
  id: string;
  type: "tool";
  name: string;
  status: "running" | "done" | "failed";
  summary: ToolSummary;
  preview?: ToolResultPreview;
  rawArguments?: string;
  rawOutput?: string;
};

export type ToolSummary =
  | { kind: "search"; title: string; detail: string }
  | { kind: "fetch"; title: string; detail: string }
  | { kind: "file"; title: string; detail: string }
  | { kind: "generic"; title: string; detail: string };

export type ToolResultPreview =
  | {
      kind: "searchResults";
      resultCount: number;
      results: SearchResultPreview[];
    }
  | {
      kind: "fetchResult";
      url?: string;
      domain?: string;
      title?: string;
      detail: string;
    }
  | {
      kind: "text";
      detail: string;
    };

export type SearchResultPreview = {
  title: string;
  url?: string;
  domain?: string;
  snippet?: string;
};

export function appendReasoningDelta(events: ActivityTraceEvent[], delta: string): ActivityTraceEvent[] {
  if (delta === "") return events;
  const last = events.at(-1);
  if (last?.type === "reasoning" && last.status === "running") {
    return [...events.slice(0, -1), { ...last, content: last.content + delta }];
  }
  const reasoningCount = events.filter((event) => event.type === "reasoning").length + 1;
  return [
    ...events,
    {
      id: `reasoning-${reasoningCount}`,
      type: "reasoning",
      content: delta,
      status: "running",
    },
  ];
}

export function upsertTraceToolCall(events: ActivityTraceEvent[], event: ToolCallEvent): ActivityTraceEvent[] {
  const next = events.filter((item) => item.id !== event.id);
  return [
    ...next,
    {
      id: event.id,
      type: "tool",
      name: event.name,
      status: "running",
      summary: summarizeToolCall(event.name, event.arguments),
      rawArguments: event.arguments,
    },
  ];
}

export function upsertTraceToolResult(events: ActivityTraceEvent[], event: ToolResultEvent): ActivityTraceEvent[] {
  return events.map((item) => {
    if (item.type !== "tool" || item.id !== event.id) return item;
    const failed = event.content.startsWith("tool failed");
    return {
      ...item,
      status: failed ? "failed" : "done",
      preview: summarizeToolResult(item, event.content),
      rawOutput: event.content,
    };
  });
}

export function completeTrace(events: ActivityTraceEvent[]): ActivityTraceEvent[] {
  return events.map((event) => {
    if (event.status !== "running") return event;
    return { ...event, status: "done" };
  });
}

export function summarizeTrace(events: ActivityTraceEvent[]): string {
  const tools = events.filter((event): event is ActivityTraceToolEvent => event.type === "tool");
  const searches = tools.filter((event) => event.summary.kind === "search").length;
  const reads = tools.filter((event) => event.summary.kind === "fetch").length;
  const failures = tools.filter((event) => event.status === "failed").length;
  const otherTools = tools.filter(
    (event) => event.status !== "failed" && event.summary.kind !== "search" && event.summary.kind !== "fetch",
  ).length;
  const parts: string[] = [];
  if (searches > 0) parts.push(`Searched ${searches} ${searches === 1 ? "query" : "queries"}`);
  if (reads > 0) parts.push(`read ${reads} ${reads === 1 ? "page" : "pages"}`);
  if (otherTools > 0) parts.push(`used ${otherTools} ${otherTools === 1 ? "tool" : "tools"}`);
  if (failures > 0) parts.push(`${failures} ${failures === 1 ? "tool" : "tools"} failed`);
  return parts.length > 0 ? parts.join(" · ") : "Reviewed work";
}

export function summarizeToolCall(name: string, rawArguments: string): ToolSummary {
  const args = parseJSONRecord(rawArguments);
  const query = stringValue(args, ["query", "q", "search", "searchQuery"]);
  if (isSearchTool(name) || query !== undefined) {
    return { kind: "search", title: query ?? readableToolName(name), detail: readableToolName(name) };
  }
  const url = stringValue(args, ["url", "uri", "href"]);
  if (isFetchTool(name) || url !== undefined) {
    return {
      kind: "fetch",
      title: url !== undefined ? domainFromURL(url) ?? url : readableToolName(name),
      detail: url ?? readableToolName(name),
    };
  }
  const file = stringValue(args, ["filename", "file", "path", "displayFilename"]);
  if (file !== undefined) {
    return { kind: "file", title: file, detail: readableToolName(name) };
  }
  return { kind: "generic", title: readableToolName(name), detail: readableToolName(name) };
}

export function summarizeToolResult(tool: ActivityTraceToolEvent, rawOutput: string): ToolResultPreview {
  const parsed = parseJSONValue(rawOutput);
  const searchResults = extractSearchResults(parsed);
  if (searchResults.length > 0 || isSearchTool(tool.name)) {
    return {
      kind: "searchResults",
      resultCount: searchResults.length,
      results: searchResults.slice(0, 6),
    };
  }
  const text = truncateText(rawOutput.trim(), 500);
  if (tool.summary.kind === "fetch") {
    const url = tool.rawArguments !== undefined ? stringValue(parseJSONRecord(tool.rawArguments), ["url", "uri", "href"]) : undefined;
    return {
      kind: "fetchResult",
      url,
      domain: url !== undefined ? domainFromURL(url) : undefined,
      detail: text,
    };
  }
  return { kind: "text", detail: text };
}

export function domainFromURL(value: string): string | undefined {
  try {
    return new URL(value).hostname.replace(/^www\./, "");
  } catch {
    return undefined;
  }
}

export function faviconURL(value: string): string | undefined {
  const domain = domainFromURL(value);
  return domain === undefined ? undefined : `https://www.google.com/s2/favicons?domain=${encodeURIComponent(domain)}&sz=32`;
}

function isSearchTool(name: string): boolean {
  return /search|tavily|web/i.test(name);
}

function isFetchTool(name: string): boolean {
  return /fetch|crawl|read|browser/i.test(name);
}

function readableToolName(name: string): string {
  return name.replace(/__/g, " ").replace(/_/g, " ").trim();
}

function stringValue(record: Record<string, unknown>, keys: string[]): string | undefined {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "string" && value.trim() !== "") return value;
  }
  return undefined;
}

function parseJSONRecord(value: string): Record<string, unknown> {
  const parsed = parseJSONValue(value);
  return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {};
}

function parseJSONValue(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    return undefined;
  }
}

function extractSearchResults(value: unknown): SearchResultPreview[] {
  const candidates = Array.isArray(value)
    ? value
    : value !== null && typeof value === "object" && Array.isArray((value as { results?: unknown }).results)
      ? (value as { results: unknown[] }).results
      : [];
  return candidates.flatMap((item) => {
    if (item === null || typeof item !== "object") return [];
    const record = item as Record<string, unknown>;
    const title = typeof record.title === "string" ? record.title : undefined;
    const url = typeof record.url === "string" ? record.url : typeof record.href === "string" ? record.href : undefined;
    if (title === undefined && url === undefined) return [];
    return [
      {
        title: title ?? url ?? "Result",
        url,
        domain: url !== undefined ? domainFromURL(url) : undefined,
        snippet: typeof record.snippet === "string" ? record.snippet : typeof record.content === "string" ? record.content : undefined,
      },
    ];
  });
}

function truncateText(value: string, maxLength: number): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}...` : value;
}
