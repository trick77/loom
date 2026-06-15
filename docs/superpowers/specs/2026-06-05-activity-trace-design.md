# Activity Trace Design

## Goal

Lume should present tool calls and model reasoning as one chronological work trace instead of two separate panels. The user should be able to see what Lume is doing, what data it is handling, and what it found without reading raw MCP tool names such as `fetch__fetch`.

The trace is not a second answer. It is provenance for the answer and should stay visually subordinate to the assistant message.

## Current Behavior

The frontend currently receives separate stream events for reasoning and tools:

- `assistant_delta`
- `assistant_reasoning_delta`
- `tool_call`
- `tool_result`

`ChatShell.tsx` renders these as separate `ThinkingPanel` and `ToolActivityPanel` elements. Tool activity is stored as raw tool names and raw result output, with completed tool activity attached to the final assistant message so it remains visible after streaming completes.

This preserves information but loses chronology. The user sees thinking and tools as separate UI concepts even though the model experience is a loop of reasoning, action, observation, and follow-up reasoning.

## Desired User Experience

Replace the separate thinking and tools elements with one collapsible Activity Trace rendered before the assistant answer.

While the assistant is working:

- The Activity Trace is expanded.
- The header uses the existing active thinking sweep.
- The sweep means only one thing: this assistant turn is still in progress.
- Reasoning snippets and tool events appear in chronological order.
- Tool rows summarize the actual data being handled, such as search queries, URLs, filenames, or artifact names.
- Search-like tools show result count and compact result previews when possible.
- Fetch or crawl tools show URL, domain, page title/status, and compact observations when possible.

After the assistant message finalizes:

- The Activity Trace auto-collapses.
- The header becomes static with no thinking sweep.
- The collapsed header summarizes completed work, for example `Searched 2 queries · read 4 pages · used 3 tools`.
- Failed tools are reflected in the summary, for example `Searched 1 query · 1 tool failed`.
- The user can expand the historical trace above the answer.
- Expanded completed traces must read as history, not as ongoing work.

## Transcript Order

The trace appears before the assistant answer.

This keeps the timeline logical:

1. User asks.
2. Lume works.
3. Lume answers.

When a completed assistant message has stored trace data, rendering should keep the trace directly above that assistant message content.

## Visual Structure

Use one compact vertical trace element that feels native to the transcript rather than like a nested panel:

- No heavy outer border or background for the expanded trace.
- A header row with the status circle, status text or summary, and disclosure chevron grouped together on the left.
- The disclosure chevron sits directly next to `Thinking` or the completed summary, not at the far right.
- A thin vertical line or equivalent alignment cue for chronological steps.
- Reasoning steps use a marker that matches the existing 16px thinking status circle.
- Web/search steps use a standalone tilted globe icon, slightly darker than primary text, with a thin stroke.
- File, artifact, image, fetch/read, generic tool, success, and failure steps use equivalent stroke-style icons.
- Status pills for `Running`, `Done`, and `Failed` share the same visual size.
- Lightly framed previews appear inside relevant tool steps only.
- Search result count appears outside the result preview box, top-right aligned above it.
- Scrollable previews use the same scrollbar styling as the sidebar.

Avoid nested full cards. The trace should feel like one transcript timeline, not a dashboard inside a chat bubble.

## Trace Model

The implementation may change the backend stream schema if that produces a cleaner and more reliable Activity Trace. The frontend should still render from a unified trace model:

```ts
type ActivityTraceEvent =
  | {
      type: "reasoning";
      id: string;
      content: string;
      status: "running" | "done";
    }
  | {
      type: "tool";
      id: string;
      name: string;
      status: "running" | "done" | "failed";
      summary: ToolSummary;
      preview?: ToolResultPreview;
      rawArguments?: string;
      rawOutput?: string;
    };
```

Reasoning deltas can be grouped into readable blocks between tool events instead of creating a new row for every delta.

Tool call and result events should update the same trace item by `id`.

Prefer backend-provided normalized display metadata whenever raw MCP arguments or results are too inconsistent for a reliable UI. The raw tool name, arguments, and output should still be preserved for debugging and fallback rendering.

## Tool Summaries

Known tool families should get specialized summaries:

- Search tools: query, result count, result rows with title, domain, snippet, and favicon when a URL is available.
- Fetch/read tools: URL, domain, page title or status when available, and at least a short text observation from the fetched content.
- File and artifact tools: filename, MIME type, size, operation, and generated download context when available.
- Image generation tools: prompt summary or generated filename, provider/model, dimensions when available, and an image thumbnail or artifact reference.
- Unknown MCP tools: readable tool name, sanitized argument summary, status, and expandable raw detail.

Raw arguments and raw output remain available behind a secondary detail toggle for debugging and transparency, but they should not be the primary visible UI.

## Favicons

Favicons should be derived client-side from result domains when URLs are present. Use Google's favicon service directly for v1:

```text
https://www.google.com/s2/favicons?domain=example.com&sz=32
```

Use the domain parameter instead of the full URL. Let the browser cache naturally. Do not prefetch favicons for hidden or collapsed result rows.

The UI must still be useful without favicons. Domain text is the fallback source of truth, and broken favicon requests should degrade to a generated domain badge or neutral placeholder.

## Accessibility

The active Activity Trace should keep `role="status"` and `aria-live="polite"` semantics equivalent to the current active thinking panel.

The collapsed/expanded state must use a real button with `aria-expanded`.

Completed traces should not use live-region semantics.

## Error Handling

Tool failures should stay visible in both collapsed and expanded states.

If a tool result cannot be parsed into a structured preview, render the best available summary and keep raw output behind the detail toggle.

Malformed tool arguments must not break rendering. The fallback summary should use the raw tool name and a safe generic label.

## Testing

Add frontend tests that cover:

- Active trace renders before streaming answer text.
- Reasoning and tool events appear in chronological order.
- Tool calls show meaningful handled data, such as a search query, instead of only the raw tool name.
- Completed assistant messages auto-collapse their trace.
- The active thinking sweep is present only while the turn is running.
- Completed traces can be expanded to inspect historical details.
- Failed tools are represented in the collapsed summary.
- Unknown tools fall back without crashing.

If the backend stream schema changes, add backend tests for the emitted trace/display metadata and frontend stream parser tests for the new event shape.

Rendered browser verification is required for the final implementation because this is a visual chat-flow change.

## Non-Goals

- No MCP protocol changes.
- No attempt to perfectly parse every possible third-party tool output.
- No large redesign of the assistant message layout beyond replacing the separate thinking/tool panels with the Activity Trace.

## Future Extension

If backend-provided display metadata is not implemented immediately, the frontend trace model should still leave room for it. If Lume normalizes tool calls server-side, the Activity Trace should consume that metadata instead of duplicating parsing heuristics in the browser.
