# Activity Trace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Spark's separate thinking and tool panels with one collapsible chronological Activity Trace that appears before the assistant answer.

**Architecture:** Keep the existing SSE event stream initially and normalize reasoning/tool events into a frontend `ActivityTraceEvent[]` model. Render one `ActivityTracePanel` for active turns and completed assistant messages, with specialized summaries for search/fetch-like tools and a safe generic fallback. Add backend display metadata only if the frontend cannot reliably summarize current tool payloads.

**Tech Stack:** React 19, TypeScript, Vite, Vitest, Testing Library, Tailwind v4 themed CSS variables, existing Spark SSE API.

**Command Context:** Run `npm ...` commands from `frontend/`. Run `git ...` commands from the repository root unless the command explicitly says otherwise.

---

## File Structure

- Create `frontend/src/activityTrace.ts`
  - Owns trace event types, reducers, summary extraction, raw fallback handling, URL/domain parsing, and completed-summary generation.
- Create `frontend/src/activityTrace.test.ts`
  - Unit tests for reasoning grouping, tool call/result updates, search query extraction, result preview extraction, failed tool summaries, malformed JSON fallback, and favicon/domain helpers.
- Modify `frontend/src/ChatShell.tsx`
  - Replace `ToolActivity` state with `ActivityTraceEvent` state.
  - Replace `ThinkingPanel` and `ToolActivityPanel` with `ActivityTracePanel`.
  - Preserve completed trace data on assistant messages.
  - Keep trace before assistant answer.
- Modify `frontend/src/index.css`
  - Rename/extend thinking-panel CSS into trace-panel CSS.
  - Keep the active thinking sweep only for active traces.
  - Add compact timeline and result-preview styles.
- Modify `frontend/src/App.test.tsx`
  - Update current thinking/tool assertions to Activity Trace behavior.
  - Add chronology, auto-collapse, completed expansion, failure summary, and fallback tests.
- Optional backend files if required after frontend parser tests:
  - `backend/internal/httpapi/message_stream_handlers.go`
  - Backend tests adjacent to stream handlers.
  - Only add these if current `tool_call.arguments` / `tool_result.content` do not carry enough structured data.

---

### Task 1: Add Activity Trace Model

**Files:**
- Create: `frontend/src/activityTrace.ts`
- Create: `frontend/src/activityTrace.test.ts`

- [ ] **Step 1: Write failing tests for trace model basics**

Create `frontend/src/activityTrace.test.ts`:

```ts
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
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
npm test -- --run src/activityTrace.test.ts
```

Expected: FAIL because `src/activityTrace.ts` does not exist.

- [ ] **Step 3: Implement the minimal trace model**

Create `frontend/src/activityTrace.ts`:

```ts
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

export function upsertTraceToolCall(
  events: ActivityTraceEvent[],
  event: ToolCallEvent,
): ActivityTraceEvent[] {
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

export function upsertTraceToolResult(
  events: ActivityTraceEvent[],
  event: ToolResultEvent,
): ActivityTraceEvent[] {
  return events.map((item) => {
    if (item.type !== "tool" || item.id !== event.id) return item;
    const failed = event.content.startsWith("tool failed");
    return {
      ...item,
      status: failed ? "failed" : "done",
      preview: summarizeToolResult(item.name, event.content),
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
  const parts: string[] = [];
  if (searches > 0) parts.push(`Searched ${searches} ${searches === 1 ? "query" : "queries"}`);
  if (reads > 0) parts.push(`read ${reads} ${reads === 1 ? "page" : "pages"}`);
  const otherTools = tools.length - searches - reads;
  if (otherTools > 0) parts.push(`used ${otherTools} ${otherTools === 1 ? "tool" : "tools"}`);
  if (failures > 0) parts.push(`${failures} tool ${failures === 1 ? "failed" : "failed"}`);
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
    return { kind: "fetch", title: url !== undefined ? domainFromURL(url) ?? url : readableToolName(name), detail: url ?? readableToolName(name) };
  }
  const file = stringValue(args, ["filename", "file", "path", "displayFilename"]);
  if (file !== undefined) {
    return { kind: "file", title: file, detail: readableToolName(name) };
  }
  return { kind: "generic", title: readableToolName(name), detail: readableToolName(name) };
}

export function summarizeToolResult(name: string, rawOutput: string): ToolResultPreview {
  const parsed = parseJSONValue(rawOutput);
  const searchResults = extractSearchResults(parsed);
  if (searchResults.length > 0 || isSearchTool(name)) {
    return {
      kind: "searchResults",
      resultCount: searchResults.length,
      results: searchResults.slice(0, 6),
    };
  }
  const text = rawOutput.trim();
  return { kind: "text", detail: text.length > 500 ? `${text.slice(0, 500)}...` : text };
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
  return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed)
    ? parsed as Record<string, unknown>
    : {};
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
    return [{
      title: title ?? url ?? "Result",
      url,
      domain: url !== undefined ? domainFromURL(url) : undefined,
      snippet: typeof record.snippet === "string" ? record.snippet : typeof record.content === "string" ? record.content : undefined,
    }];
  });
}
```

- [ ] **Step 4: Run model tests**

Run:

```bash
npm test -- --run src/activityTrace.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd ..
git add frontend/src/activityTrace.ts frontend/src/activityTrace.test.ts
git commit -m "feat: add activity trace model"
```

---

### Task 2: Replace Streaming State With Activity Trace State

**Files:**
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Write failing integration test for active trace chronology**

In `frontend/src/App.test.tsx`, replace the old `keeps the thinking panel visible during tool activity before assistant output` test with:

```ts
test("shows active activity trace with reasoning and tool activity before assistant output", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
      controller.enqueue(
        new TextEncoder().encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_delta\ndata: {"content":"I should search current sources."}\n\n'),
  );
  streamController.current?.enqueue(
    new TextEncoder().encode('event: tool_call\ndata: {"id":"call_1","name":"search__web","arguments":"{\\"query\\":\\"agentgateway kgateway\\"}"}\n\n'),
  );

  const trace = await screen.findByRole("status", { name: /spark activity trace/i });
  expect(within(trace).getByRole("button", { name: /hide activity/i })).toBeInTheDocument();
  expect(within(trace).getByText("Thinking")).toBeInTheDocument();
  expect(within(trace).getByText("I should search current sources.")).toBeInTheDocument();
  expect(within(trace).getByText("agentgateway kgateway")).toBeInTheDocument();
  expect(within(trace).getByText("Running")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run the failing integration test**

Run:

```bash
npm test -- --run src/App.test.tsx -t "shows active activity trace"
```

Expected: FAIL because `ActivityTracePanel` does not exist and the old separate panels render.

- [ ] **Step 3: Replace state helpers in `ChatShell.tsx`**

Update imports:

```ts
import {
  appendReasoningDelta,
  completeTrace,
  summarizeTrace,
  upsertTraceToolCall,
  upsertTraceToolResult,
  type ActivityTraceEvent,
  type ActivityTraceToolEvent,
} from "./activityTrace";
```

Replace the local tool types:

```ts
type MessageWithActivityTrace = Message & {
  activityTrace?: ActivityTraceEvent[];
};
```

Replace state:

```ts
const [messages, setMessages] = useState<MessageWithActivityTrace[]>([]);
const [activityTrace, setActivityTrace] = useState<ActivityTraceEvent[]>([]);
const activityTraceRef = useRef<ActivityTraceEvent[]>([]);
```

Replace update helpers:

```ts
const updateActivityTrace = useCallback((updater: (current: ActivityTraceEvent[]) => ActivityTraceEvent[]) => {
  const next = updater(activityTraceRef.current);
  activityTraceRef.current = next;
  setActivityTrace(next);
}, []);

const clearActivityTrace = useCallback(() => {
  activityTraceRef.current = [];
  setActivityTrace([]);
}, []);
```

Update stream handlers:

```ts
onReasoningDelta: (delta) => {
  if (!isCurrentThread()) return;
  setStreamingReasoning((current) => current + delta);
  updateActivityTrace((current) => appendReasoningDelta(current, delta));
},
onToolCall: (event) => {
  if (!isCurrentThread()) return;
  updateActivityTrace((current) => upsertTraceToolCall(current, event));
},
onToolResult: (event) => {
  if (!isCurrentThread()) return;
  updateActivityTrace((current) => upsertTraceToolResult(current, event));
},
onAssistantMessage: (message) => {
  if (!isCurrentThread()) return;
  const completedTrace = completeTrace(activityTraceRef.current);
  setMessages((current) => [
    ...current,
    completedTrace.length > 0 ? { ...message, activityTrace: completedTrace } : message,
  ]);
  setStreamingText("");
  setStreamingReasoning("");
  setStreamingArtifacts([]);
  clearActivityTrace();
},
```

Update all `clearToolEvents()` call sites to `clearActivityTrace()`.

- [ ] **Step 4: Add minimal `ActivityTracePanel` renderer**

Replace `ThinkingPanel` and `ToolActivityPanel` usage in `ChatPanel`:

```tsx
{message.role === "assistant" && message.activityTrace !== undefined && (
  <ActivityTracePanel events={message.activityTrace} active={false} />
)}
{message.role === "assistant" && message.activityTrace === undefined && message.reasoningContent && (
  <ActivityTracePanel
    events={[{ id: `${message.id}-reasoning`, type: "reasoning", content: message.reasoningContent, status: "done" }]}
    active={false}
  />
)}
```

For streaming state:

```tsx
{showActiveActivityTrace && <ActivityTracePanel events={activityTrace} active={true} />}
```

Define:

```tsx
function ActivityTracePanel({
  events,
  active,
}: {
  events: ActivityTraceEvent[];
  active: boolean;
}) {
  const [expanded, setExpanded] = useState(active);
  useEffect(() => {
    if (active) setExpanded(true);
  }, [active]);
  if (events.length === 0 && !active) return null;
  const summary = active ? "Thinking" : summarizeTrace(events);
  return (
    <div
      aria-label={active ? "Spark activity trace" : undefined}
      aria-live={active ? "polite" : undefined}
      className="spark-activity-trace"
      role={active ? "status" : undefined}
    >
      <button
        aria-expanded={expanded}
        aria-label={expanded ? "Hide activity" : "Show activity"}
        className="spark-activity-trace-toggle"
        type="button"
        onClick={() => setExpanded((current) => !current)}
      >
        <span className="spark-thinking-panel-label">
          <span className={active ? "spark-thinking-status-active" : "spark-thinking-status-complete"} aria-hidden="true" />
          {active ? (
            <span className="spark-thinking-label-active" data-text="Thinking">Thinking</span>
          ) : (
            <span>{summary}</span>
          )}
        </span>
        <span aria-hidden="true" className={expanded ? "spark-thinking-chevron-expanded" : "spark-thinking-chevron"} />
      </button>
      {expanded && (
        <div className="spark-activity-trace-body">
          {events.map((event) => (
            <ActivityTraceRow key={event.id} event={event} />
          ))}
        </div>
      )}
    </div>
  );
}
```

Add row renderer:

```tsx
function ActivityTraceRow({ event }: { event: ActivityTraceEvent }) {
  if (event.type === "reasoning") {
    return (
      <div className="spark-activity-trace-row">
        <span className="spark-activity-trace-icon" aria-hidden="true">◌</span>
        <Markdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>
          {event.content.trim()}
        </Markdown>
      </div>
    );
  }
  const status = activityToolStatusMeta(event);
  return (
    <div className="spark-activity-trace-row">
      <span className="spark-activity-trace-icon" aria-hidden="true">{event.summary.kind === "search" ? "⌕" : "↗"}</span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-3">
          <span className="min-w-0 truncate text-[#d6d3ca]">{event.summary.title}</span>
          <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] ${status.className}`}>{status.label}</span>
        </div>
        <div className="truncate text-[#88857d]">{event.summary.detail}</div>
      </div>
    </div>
  );
}

function activityToolStatusMeta(event: ActivityTraceToolEvent): { label: string; className: string } {
  if (event.status === "failed") return { label: "Failed", className: "bg-[#b85c52] text-[#fffaf2]" };
  if (event.status === "running") return { label: "Running", className: "bg-[#363632] text-[#c7c5bd]" };
  return { label: "Done", className: "bg-[#363632] text-[#c7c5bd]" };
}
```

- [ ] **Step 5: Run integration test**

Run:

```bash
npm test -- --run src/App.test.tsx -t "shows active activity trace"
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd ..
git add frontend/src/ChatShell.tsx frontend/src/App.test.tsx
git commit -m "feat: render active activity trace"
```

---

### Task 3: Render Completed Traces Collapsed Before Answers

**Files:**
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Write failing completed-trace test**

Replace the old `keeps completed tool activity visible with the assistant answer` test with:

```ts
test("keeps completed activity trace collapsed before the assistant answer", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Search for updates","createdAt":"2026-05-30T00:00:00Z"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_reasoning_delta\ndata: {"content":"I should search current sources."}\n\n'));
      controller.enqueue(encoder.encode('event: tool_call\ndata: {"id":"call_1","name":"search__web","arguments":"{\\"query\\":\\"agentgateway kgateway\\"}"}\n\n'));
      controller.enqueue(encoder.encode('event: tool_result\ndata: {"id":"call_1","name":"search__web","content":"{\\"results\\":[{\\"title\\":\\"Agentgateway\\",\\"url\\":\\"https://agentgateway.dev\\",\\"snippet\\":\\"Next generation proxy\\"}]}"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"I found the update.","createdAt":"2026-05-30T00:00:01Z"}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Search for updates" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  const answer = await screen.findByText("I found the update.");
  const toggle = screen.getByRole("button", { name: /show activity/i });
  expect(toggle).toHaveTextContent("Searched 1 query");
  expect(screen.queryByRole("status", { name: /spark activity trace/i })).not.toBeInTheDocument();
  expect(screen.queryByText("agentgateway kgateway")).not.toBeInTheDocument();

  fireEvent.click(toggle);

  expect(await screen.findByText("I should search current sources.")).toBeInTheDocument();
  expect(screen.getByText("agentgateway kgateway")).toBeInTheDocument();
  expect(screen.getByText("Agentgateway")).toBeInTheDocument();
  expect(toggle.compareDocumentPosition(answer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});
```

- [ ] **Step 2: Run failing test**

Run:

```bash
npm test -- --run src/App.test.tsx -t "completed activity trace"
```

Expected: FAIL until completed traces initialize collapsed and result previews render.

- [ ] **Step 3: Make completed traces collapsed by default**

In `ActivityTracePanel`, initialize expansion from `active`:

```tsx
const [expanded, setExpanded] = useState(active);
useEffect(() => {
  if (active) setExpanded(true);
  if (!active) setExpanded(false);
}, [active]);
```

Keep active traces expanded and completed traces collapsed.

- [ ] **Step 4: Add search result preview rendering**

Extend `ActivityTraceRow` tool branch:

```tsx
{event.preview?.kind === "searchResults" && event.preview.results.length > 0 && (
  <div className="spark-activity-result-list">
    <div className="mb-1 text-right text-[#88857d]">
      {event.preview.resultCount} {event.preview.resultCount === 1 ? "result" : "results"}
    </div>
    {event.preview.results.map((result, index) => (
      <div key={`${result.url ?? result.title}-${index}`} className="spark-activity-result-row">
        {result.url !== undefined ? (
          <img alt="" className="spark-activity-favicon" src={faviconURL(result.url)} />
        ) : (
          <span className="spark-activity-favicon" aria-hidden="true" />
        )}
        <div className="min-w-0">
          <div className="truncate text-[#d6d3ca]">{result.title}</div>
          {result.snippet !== undefined && <div className="truncate text-[#aaa79e]">{result.snippet}</div>}
        </div>
        {result.domain !== undefined && <div className="shrink-0 text-[#88857d]">{result.domain}</div>}
      </div>
    ))}
  </div>
)}
```

Add `faviconURL` to imports from `activityTrace`.

- [ ] **Step 5: Run completed-trace test**

Run:

```bash
npm test -- --run src/App.test.tsx -t "completed activity trace"
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd ..
git add frontend/src/ChatShell.tsx frontend/src/App.test.tsx
git commit -m "feat: collapse completed activity traces"
```

---

### Task 4: Add Failure and Unknown Tool Fallback Tests

**Files:**
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/activityTrace.ts`

- [ ] **Step 1: Write failing failure-summary test**

Update `surfaces the server error and keeps failed tool activity visible`:

```ts
test("surfaces the server error and keeps failed activity trace visible", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: tool_call\ndata: {"id":"call_1","name":"search__web","arguments":"{\\"query\\":\\"agentgateway\\"}"}\n\n'));
      controller.enqueue(encoder.encode('event: tool_result\ndata: {"id":"call_1","name":"search__web","content":"tool failed: timeout"}\n\n'));
      controller.enqueue(encoder.encode('event: error\ndata: {"error":"llm is not configured"}\n\n'));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("llm is not configured")).toBeInTheDocument();
  const trace = screen.getByRole("status", { name: /spark activity trace/i });
  expect(within(trace).getByText("agentgateway")).toBeInTheDocument();
  expect(within(trace).getByText("Failed")).toBeInTheDocument();
});
```

- [ ] **Step 2: Write failing unknown-tool fallback test**

Add:

```ts
test("renders unknown tool calls with safe fallback details", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: tool_call\ndata: {"id":"call_1","name":"custom__lookup","arguments":"not-json"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Done.","createdAt":"2026-05-30T00:00:01Z"}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("Done.")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: /show activity/i }));
  expect(await screen.findByText("custom lookup")).toBeInTheDocument();
  expect(screen.getByText("Done")).toBeInTheDocument();
});
```

- [ ] **Step 3: Run failing fallback tests**

Run:

```bash
npm test -- --run src/App.test.tsx -t "failed activity trace|unknown tool"
```

Expected: FAIL if failed active traces or fallback labels are not represented correctly.

- [ ] **Step 4: Fix active failure rendering**

Ensure `upsertTraceToolResult` sets failed status when output begins with `tool failed`, and `ActivityTracePanel` can render active traces after stream errors because `sendError` must not clear `activityTrace`.

Use this active trace visibility condition:

```ts
const hasActiveTrace = activityTrace.length > 0;
const showActiveActivityTrace =
  hasActiveTrace || (isSending && sendError === "" && streamingText === "");
```

When the stream fails, leave `activityTrace` intact until the next send or thread switch.

- [ ] **Step 5: Run fallback tests**

Run:

```bash
npm test -- --run src/App.test.tsx -t "failed activity trace|unknown tool"
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd ..
git add frontend/src/App.test.tsx frontend/src/ChatShell.tsx frontend/src/activityTrace.ts
git commit -m "feat: handle activity trace failures"
```

---

### Task 5: Final Styling and Existing Test Migration

**Files:**
- Modify: `frontend/src/index.css`
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Update CSS classes**

In `frontend/src/index.css`, keep the sweep keyframes and status dot styles, but replace panel/body class names with:

```css
.spark-activity-trace {
  max-width: 46rem;
  overflow: hidden;
  border: 1px solid #3e3d39;
  border-radius: 0.5rem;
  background: #282826;
  color: #9b9790;
  font-family: var(--font-sans);
  font-size: 0.8125rem;
  line-height: 1.45rem;
}

.spark-activity-trace-toggle {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.625rem 0.875rem;
  color: #c7c5bd;
  text-align: left;
  transition: color 0.16s ease;
}

.spark-activity-trace-toggle:hover {
  color: #f3f0e8;
}

.spark-activity-trace-body {
  border-top: 1px solid #3e3d39;
  padding: 0.75rem 0.875rem 0.875rem;
}

.spark-activity-trace-row {
  position: relative;
  display: flex;
  gap: 0.75rem;
  padding: 0.2rem 0 0.65rem;
}

.spark-activity-trace-row:not(:last-child)::before {
  position: absolute;
  bottom: -0.15rem;
  left: 0.45rem;
  top: 1.55rem;
  width: 1px;
  background: #4a4741;
  content: "";
}

.spark-activity-trace-icon {
  display: grid;
  width: 1rem;
  height: 1rem;
  flex: 0 0 auto;
  place-items: center;
  color: #88857d;
}

.spark-activity-result-list {
  margin-top: 0.5rem;
  max-height: 10rem;
  overflow: auto;
  border: 1px solid #4a4741;
  border-radius: 0.375rem;
  background: #1f1f1d;
  padding: 0.45rem;
}

.spark-activity-result-row {
  display: grid;
  grid-template-columns: 1rem minmax(0, 1fr) auto;
  align-items: center;
  gap: 0.55rem;
  padding: 0.3rem 0.35rem;
}

.spark-activity-favicon {
  width: 1rem;
  height: 1rem;
  border-radius: 0.2rem;
}
```

Update markdown selector groups from `.spark-thinking-panel-body` to include `.spark-activity-trace-body`.

- [ ] **Step 2: Migrate old tests**

Rename and update tests:

- `renders streamed reasoning in a collapsed thinking panel` becomes `renders completed reasoning in a collapsed activity trace`.
- `shows the thinking panel while waiting for the first assistant output` becomes `shows active activity trace while waiting for assistant output`.
- `keeps streamed reasoning visible while assistant text is streaming` becomes `keeps active activity trace visible while assistant text is streaming`.
- `hides the thinking panel when the stream fails` becomes `hides empty activity trace when the stream fails`.

Use role/name assertions for `Show activity`, `Hide activity`, and `Spark activity trace`.

- [ ] **Step 3: Run full frontend tests**

Run:

```bash
npm test -- --run
```

Expected: PASS.

- [ ] **Step 4: Run typecheck/build**

Run:

```bash
npm run build
```

Expected: PASS. After build, restore generated backend web placeholders if the build touched them:

```bash
git checkout -- ../backend/web/dist/.gitkeep ../backend/web/dist/index.html
```

- [ ] **Step 5: Commit**

```bash
cd ..
git add frontend/src/index.css frontend/src/ChatShell.tsx frontend/src/App.test.tsx
git commit -m "feat: polish activity trace ui"
```

---

### Task 6: Browser Verification

**Files:**
- No source changes expected unless visual QA finds an issue.

- [ ] **Step 1: Start dev server**

Run from `frontend/`:

```bash
npm run dev -- --host 127.0.0.1 --port 5174
```

Expected: Vite reports local URL `http://127.0.0.1:5174/`.

- [ ] **Step 2: Open Browser preview**

Use the Browser plugin to open:

```text
http://127.0.0.1:5174/
```

- [ ] **Step 3: Verify layout manually**

Check desktop and mobile widths:

- Active trace is expanded and visually subordinate to the final answer.
- Completed trace collapses before the answer.
- The thinking sweep appears only while active.
- Search result rows do not overflow their container.
- Favicon failures do not leave broken-looking UI.
- Transcript ordering is user message, work trace, answer.

- [ ] **Step 4: Fix visual issues if found**

If text overlaps or the trace is too heavy, adjust only `frontend/src/index.css` and rerun:

```bash
npm test -- --run src/App.test.tsx
npm run build
```

- [ ] **Step 5: Commit visual fixes**

```bash
cd ..
git add frontend/src/index.css frontend/src/App.test.tsx frontend/src/ChatShell.tsx
git commit -m "fix: refine activity trace rendering"
```

Skip this commit if no source changes were needed.

---

### Task 7: Final Verification

**Files:**
- No source changes expected.

- [ ] **Step 1: Run frontend test suite**

```bash
npm test -- --run
```

Expected: PASS.

- [ ] **Step 2: Run TypeScript and Vite build**

```bash
npm run build
```

Expected: PASS.

- [ ] **Step 3: Restore build placeholders if needed**

```bash
git checkout -- ../backend/web/dist/.gitkeep ../backend/web/dist/index.html
```

- [ ] **Step 4: Check diff**

```bash
git status --short
git diff --stat
```

Expected: only intentional frontend source/test/CSS files and docs/plans are changed or committed.

- [ ] **Step 5: Report**

Report:

- Worktree path.
- Branch name.
- Commit list.
- Verification commands and results.
- Any backend schema changes made, or explicitly state none were needed.
