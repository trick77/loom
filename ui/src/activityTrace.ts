import type { ToolCallEvent, ToolResultEvent } from "./api";

const TOOL_FAILED_PREFIX = "tool failed";

export type ActivityTraceEvent =
  | {
      id: string;
      type: "reasoning";
      content: string;
      title?: string;
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
  | { kind: "search"; title: string }
  | { kind: "fetch"; title: string; url?: string }
  | { kind: "file"; title: string }
  | { kind: "generic"; title: string };

export type ToolResultPreview =
  | {
      kind: "searchResults";
      resultCount: number;
      results: SearchResultPreview[];
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
  const last = events[events.length - 1];
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
    const failed = event.content.startsWith(TOOL_FAILED_PREFIX);
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

export function normalizeActivityTrace(events: ActivityTraceEvent[] | undefined): ActivityTraceEvent[] | undefined {
  if (events === undefined || events.length === 0) return undefined;
  return events.map((event) => {
    if (event.type === "reasoning") {
      return { ...event, status: event.status === "running" ? "done" : event.status };
    }
    const summary = event.summary ?? summarizeToolCall(event.name, event.rawArguments ?? "{}");
    const normalized: ActivityTraceToolEvent = {
      ...event,
      status: event.status === "running" && event.rawOutput !== undefined ? "done" : event.status,
      summary,
    };
    if (event.rawOutput !== undefined && event.preview === undefined) {
      const failed = event.rawOutput.startsWith(TOOL_FAILED_PREFIX);
      normalized.status = failed ? "failed" : normalized.status;
      normalized.preview = summarizeToolResult(normalized, event.rawOutput);
    }
    return normalized;
  });
}

// The collapsed trace label always shows an abstract of the reasoning, never a
// tool-call statistic — the per-tool details (and failures) are available by
// expanding the activity panel. Prefer the backend-generated title of the most
// recent reasoning round; fall back to the client-side heuristic while no title
// has arrived yet.
export function summarizeTrace(events: ActivityTraceEvent[]): string {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (event.type === "reasoning" && event.title !== undefined && event.title.trim() !== "") {
      return event.title;
    }
  }
  return summarizeReasoning(events);
}

// applyReasoningTitle stamps a background-generated title onto the matching
// reasoning event, leaving the rest of the trace untouched.
export function applyReasoningTitle(events: ActivityTraceEvent[], id: string, title: string): ActivityTraceEvent[] {
  return events.map((event) =>
    event.type === "reasoning" && event.id === id ? { ...event, title } : event,
  );
}

export function summarizeToolCall(name: string, rawArguments: string): ToolSummary {
  const args = parseJSONRecord(rawArguments);
  const query = stringValue(args, ["query", "q", "search", "searchQuery"]);
  if (isSearchTool(name) || query !== undefined) {
    return { kind: "search", title: query ?? readableToolName(name) };
  }
  const url = stringValue(args, ["url", "uri", "href"]);
  if (isFetchTool(name) || url !== undefined) {
    return {
      kind: "fetch",
      title: url !== undefined ? domainFromURL(url) ?? url : readableToolName(name),
      url,
    };
  }
  const file = stringValue(args, ["filename", "file", "path", "displayFilename"]);
  if (file !== undefined) {
    return { kind: "file", title: file };
  }
  return { kind: "generic", title: readableToolName(name) };
}

export function summarizeToolResult(tool: ActivityTraceToolEvent, rawOutput: string): ToolResultPreview {
  const parsed = parseJSONValue(rawOutput);
  const searchResults = extractSearchResults(parsed);
  if (searchResults.length > 0 || isSearchTool(tool.name)) {
    return {
      kind: "searchResults",
      resultCount: searchResults.length,
      results: searchResults,
    };
  }
  const text = truncateText(rawOutput.trim(), 500);
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

function summarizeReasoning(events: ActivityTraceEvent[]): string {
  const content = events
    .filter((event) => event.type === "reasoning")
    .map((event) => event.content)
    .join(" ")
    .replace(/\s+/g, " ")
    .trim();
  if (content === "") return "Activity complete";
  const firstSentence = content.match(/^[^.!?]+[.!?]?/)?.[0] ?? content;
  const summary = stripReasoningLeadIn(firstSentence)
    .replace(/[.!?]+$/, "")
    .replace(/\s+before\s+(?:answering|responding)$/i, "")
    .trim();
  if (summary === "") return "Activity complete";
  return truncateText(summary.charAt(0).toUpperCase() + summary.slice(1), 72);
}

function stripReasoningLeadIn(value: string): string {
  return value
    .replace(/^(?:i|we)\s+should\s+compare\s+/i, "compared ")
    .replace(/^(?:i|we)\s+(?:should|need to|will|can|must|am going to|\'ll)\s+/i, "")
    .replace(/^let(?:'s| us)\s+/i, "")
    .replace(/^thinking through\s+/i, "")
    .replace(/^checking\s+/i, "checked ");
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
    const snippet = typeof record.snippet === "string" ? record.snippet : typeof record.content === "string" ? record.content : undefined;
    return [
      {
        title: title === undefined ? (url ?? "Result") : stripSearchResultMarkdown(title),
        url,
        domain: url !== undefined ? domainFromURL(url) : undefined,
        snippet: snippet === undefined ? undefined : stripSearchResultMarkdown(snippet),
      },
    ];
  });
}

function truncateText(value: string, maxLength: number): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}...` : value;
}

function stripSearchResultMarkdown(value: string): string {
  return value
    .replace(/^\s{0,3}#{1,6}\s+/gm, "")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/[*_`~]+/g, "")
    .replace(/\s+/g, " ")
    .trim();
}
