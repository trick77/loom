# Per-Bubble Token Stats Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a hover-only stats line (model · duration · tok/s · token counts · timestamp) under each assistant chat bubble, toggleable to always-visible.

**Architecture:** The only missing datum is generation duration; token counts already persist and flow to the frontend via the existing `assistant_message` SSE event and `listMessages`. We add `duration_ms` + `model` columns, thread them through `llm.StreamResult` → handler → store → `chat.Message` JSON, then add a small pure formatting module and a React component (with a localStorage-backed visibility toggle) on the frontend.

**Tech Stack:** Go (backend, SQLite via embedded migrations), React + TypeScript + Vite + Vitest (frontend), Tailwind for the hover/opacity behavior.

**Reference:** Design spec at `docs/superpowers/specs/2026-05-31-bubble-token-stats-design.md`.

**Test commands:**
- Backend: `cd backend && go test ./...`
- Frontend: `cd frontend && npm run test -- --run`

---

## File Structure

**Backend (Go):**
- Create: `backend/internal/store/migrations/0005_message_metrics.sql` — adds `duration_ms`, `model` columns.
- Modify: `backend/internal/chat/model.go` — add `DurationMs`/`Model` to `Message` and `MessageTokenUsage`.
- Modify: `backend/internal/chat/message_store.go` — INSERT + both SELECTs.
- Modify: `backend/internal/chat/scan.go` — scan the two new columns; add `nullableString`.
- Modify: `backend/internal/llm/types.go` — add `Duration`/`Model` to `StreamResult`.
- Modify: `backend/internal/llm/stream.go` — populate `Duration`/`Model` at both return sites.
- Modify: `backend/internal/httpapi/message_stream_handlers.go` — replace `messageUsageFromLLM` with `messageMetricsFromResult`; add `strPtr`.
- Test: `backend/internal/chat/store_test.go`, `backend/internal/llm/client_test.go`.

**Frontend (TS/React):**
- Modify: `frontend/src/api.ts` — extend `Message` type.
- Create: `frontend/src/metrics.ts` — pure formatting helpers.
- Create: `frontend/src/metrics.test.ts` — unit tests for helpers.
- Create: `frontend/src/MessageMetrics.tsx` — `MetricsProvider` + `MessageMetrics` component.
- Create: `frontend/src/MessageMetrics.test.tsx` — render + toggle tests.
- Modify: `frontend/src/ChatShell.tsx` — wrap transcript in `MetricsProvider`, pass message into `AssistantText`, render `MessageMetrics`.

---

## Task 1: DB migration + store persistence for duration_ms & model

**Files:**
- Create: `backend/internal/store/migrations/0005_message_metrics.sql`
- Modify: `backend/internal/chat/model.go`
- Modify: `backend/internal/chat/message_store.go` (INSERT ~line 39-63, ListMessages SELECT ~line 100, getMessage SELECT ~line 127)
- Modify: `backend/internal/chat/scan.go` (`scanMessage` ~line 70, add `nullableString`)
- Test: `backend/internal/chat/store_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/chat/store_test.go`:

```go
func TestStore_AddMessageWithUsagePersistsDurationAndModel(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	userID := insertTestUser(t, db, "alice")
	store := NewStore(db)
	thread, err := store.CreateThread(ctx, userID, CreateThreadInput{Title: "Metrics"})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}

	model := "mimo"
	message, err := store.AddMessageWithUsage(ctx, userID, thread.ID, RoleAssistant, "answer", MessageTokenUsage{
		CompletionTokens: ptr(120),
		DurationMs:       ptr(2500),
		Model:            &model,
	})
	if err != nil {
		t.Fatalf("AddMessageWithUsage() error: %v", err)
	}
	if got := intValue(message.DurationMs); got != 2500 {
		t.Fatalf("DurationMs = %d, want 2500", got)
	}
	if message.Model == nil || *message.Model != "mimo" {
		t.Fatalf("Model = %v, want \"mimo\"", message.Model)
	}

	// Survives a round-trip read (covers ListMessages SELECT path).
	messages, ok, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil || !ok {
		t.Fatalf("ListMessages() error: %v ok: %v", err, ok)
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if got := intValue(messages[0].DurationMs); got != 2500 {
		t.Fatalf("reloaded DurationMs = %d, want 2500", got)
	}
	if messages[0].Model == nil || *messages[0].Model != "mimo" {
		t.Fatalf("reloaded Model = %v, want \"mimo\"", messages[0].Model)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/chat/ -run TestStore_AddMessageWithUsagePersistsDurationAndModel`
Expected: FAIL to compile — `MessageTokenUsage` has no field `DurationMs`/`Model`; `Message` has no field `DurationMs`/`Model`.

- [ ] **Step 3: Create the migration**

Create `backend/internal/store/migrations/0005_message_metrics.sql`:

```sql
ALTER TABLE messages ADD COLUMN duration_ms INTEGER;
ALTER TABLE messages ADD COLUMN model TEXT;
```

- [ ] **Step 4: Add struct fields**

In `backend/internal/chat/model.go`, add two fields to the `Message` struct (after `ReasoningTokens`):

```go
	DurationMs       *int            `json:"durationMs,omitempty"`
	Model            *string         `json:"model,omitempty"`
```

And add two fields to the `MessageTokenUsage` struct (after `ReasoningTokens`):

```go
	DurationMs       *int
	Model            *string
```

- [ ] **Step 5: Wire the INSERT**

In `backend/internal/chat/message_store.go`, extend the INSERT in `AddMessageWithUsage`. Replace the column list, `VALUES` placeholders, and args so the statement reads:

```go
	_, err = tx.ExecContext(ctx, `
INSERT INTO messages (
    id,
    thread_id,
    user_id,
    role,
    content,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    cached_tokens,
    reasoning_tokens,
    duration_ms,
    model
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		messageID,
		threadID,
		userID,
		role,
		content,
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		usage.CachedTokens,
		usage.ReasoningTokens,
		usage.DurationMs,
		usage.Model,
	)
```

- [ ] **Step 6: Wire BOTH SELECTs**

In `backend/internal/chat/message_store.go`, append `, duration_ms, model` to the column list of **both** SELECT statements (in `ListMessages` ~line 100 and in `getMessage` ~line 127), placing them right after `reasoning_tokens` and before `created_at`. Each becomes:

```go
SELECT id, thread_id, role, content, tool_calls, citations, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, duration_ms, model, created_at
```

- [ ] **Step 7: Wire the scan**

In `backend/internal/chat/scan.go`, update `scanMessage`. Add local declarations and scan targets, and assign the new fields. The relevant additions:

Declare alongside the other nullables:

```go
	var durationMs sql.NullInt64
	var model sql.NullString
```

Insert into the `row.Scan(...)` call, between `&reasoningTokens,` and `&createdAt,`:

```go
		&durationMs,
		&model,
```

Assign after `message.ReasoningTokens = nullableInt(reasoningTokens)`:

```go
	message.DurationMs = nullableInt(durationMs)
	message.Model = nullableString(model)
```

Add a helper next to `nullableInt`:

```go
func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `cd backend && go test ./internal/chat/ -run TestStore_AddMessageWithUsagePersistsDurationAndModel`
Expected: PASS

- [ ] **Step 9: Run the full chat + store package tests**

Run: `cd backend && go test ./internal/chat/ ./internal/store/`
Expected: PASS (the migration applies cleanly; existing token tests still pass).

- [ ] **Step 10: Commit**

```bash
git add backend/internal/store/migrations/0005_message_metrics.sql backend/internal/chat/model.go backend/internal/chat/message_store.go backend/internal/chat/scan.go backend/internal/chat/store_test.go
git commit -m "feat(chat): persist message duration_ms and model"
```

---

## Task 2: Carry Duration & Model on llm.StreamResult

**Files:**
- Modify: `backend/internal/llm/types.go` (`StreamResult` ~line 82-86)
- Modify: `backend/internal/llm/stream.go` (`StreamChatWithTools` — `[DONE]` return ~line 52-59 and post-loop return ~line 124-130)
- Test: `backend/internal/llm/client_test.go`

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/llm/client_test.go`:

```go
func TestClient_StreamChatResultReportsModelAndDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	result, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("StreamChatResult() error: %v", err)
	}
	if result.Model != "mimo" {
		t.Fatalf("Model = %q, want \"mimo\"", result.Model)
	}
	if result.Duration < 0 {
		t.Fatalf("Duration = %v, want >= 0", result.Duration)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/llm/ -run TestClient_StreamChatResultReportsModelAndDuration`
Expected: FAIL to compile — `result.Model` / `result.Duration` undefined.

- [ ] **Step 3: Add fields to StreamResult**

In `backend/internal/llm/types.go`, extend the struct (add `"time"` to imports if not present — it is used elsewhere in the package, but this file currently has no imports, so add an import block):

```go
import "time"

type StreamResult struct {
	Content   string
	ToolCalls []ToolCall
	Usage     TokenUsage
	Duration  time.Duration
	Model     string
}
```

- [ ] **Step 4: Populate at both return sites**

In `backend/internal/llm/stream.go`, `StreamChatWithTools`, set the new fields on `result` before each successful return.

In the `[DONE]` branch, replace:

```go
		if payload == "[DONE]" {
			result, err := finishStream(content.String(), usage, toolCalls, toolCallOrder, onEvent)
			if err != nil {
				logInferenceFailed(ctx, c.model, time.Since(start), err)
				return result, err
			}
			logInferenceCompleted(ctx, c.model, time.Since(start), result.Usage)
			return result, nil
		}
```

with:

```go
		if payload == "[DONE]" {
			result, err := finishStream(content.String(), usage, toolCalls, toolCallOrder, onEvent)
			if err != nil {
				logInferenceFailed(ctx, c.model, time.Since(start), err)
				return result, err
			}
			result.Duration = time.Since(start)
			result.Model = c.model
			logInferenceCompleted(ctx, c.model, result.Duration, result.Usage)
			return result, nil
		}
```

And the post-loop return at the end of the function, replace:

```go
	result, err := finishStream(content.String(), usage, toolCalls, toolCallOrder, onEvent)
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return result, err
	}
	logInferenceCompleted(ctx, c.model, time.Since(start), result.Usage)
	return result, nil
}
```

with:

```go
	result, err := finishStream(content.String(), usage, toolCalls, toolCallOrder, onEvent)
	if err != nil {
		logInferenceFailed(ctx, c.model, time.Since(start), err)
		return result, err
	}
	result.Duration = time.Since(start)
	result.Model = c.model
	logInferenceCompleted(ctx, c.model, result.Duration, result.Usage)
	return result, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/llm/ -run TestClient_StreamChatResultReportsModelAndDuration`
Expected: PASS

- [ ] **Step 6: Run full llm package tests**

Run: `cd backend && go test ./internal/llm/`
Expected: PASS (existing usage/logging tests unaffected).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/llm/types.go backend/internal/llm/stream.go backend/internal/llm/client_test.go
git commit -m "feat(llm): expose generation duration and model on StreamResult"
```

---

## Task 3: Persist duration & model from the stream handler

**Files:**
- Modify: `backend/internal/httpapi/message_stream_handlers.go` (call site ~line 92; `messageUsageFromLLM` ~line 211-223; `intPtr` ~line 224)
- Test: `backend/internal/httpapi/message_stream_handlers_test.go`

This task threads the values from `assistantResult` into the persisted message. The existing handler test file already exercises the streaming endpoint; we assert the persisted assistant message carries a model and a non-negative duration.

- [ ] **Step 1: Inspect the existing handler test to reuse its harness**

Run: `cd backend && grep -nE "func Test|assistant_message|streamMessage|json.Unmarshal|chat.Message|Model|DurationMs|decodeSSE|events" internal/httpapi/message_stream_handlers_test.go | head -40`
Expected: reveals an existing test that drives the stream endpoint and decodes SSE events. Reuse its setup (server construction, fake LLM, SSE decode helper) for the new test. Note the helper names it uses.

- [ ] **Step 2: Write the failing test**

Add a test to `backend/internal/httpapi/message_stream_handlers_test.go` modeled on the existing streaming test in that file. It must: drive the send-message endpoint with a fake LLM that returns content + usage, decode the `assistant_message` SSE event into a `chat.Message`, and assert:

```go
	// `assistantMsg` is the chat.Message decoded from the "assistant_message" SSE event.
	if assistantMsg.Model == nil || *assistantMsg.Model == "" {
		t.Fatalf("assistant message Model = %v, want non-empty", assistantMsg.Model)
	}
	if assistantMsg.DurationMs == nil || *assistantMsg.DurationMs < 0 {
		t.Fatalf("assistant message DurationMs = %v, want >= 0", assistantMsg.DurationMs)
	}
```

> Adapt variable names to the existing helper (e.g. how it extracts the `assistant_message` payload). If the fake LLM in the test does not set a model, configure it to return `Model: "mimo"` on its `StreamResult` so the assertion is meaningful.

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run <NewTestName>`
Expected: FAIL — `Model`/`DurationMs` are nil because the handler does not yet persist them.

- [ ] **Step 4: Replace messageUsageFromLLM with messageMetricsFromResult**

In `backend/internal/httpapi/message_stream_handlers.go`, change the call site (~line 92) from:

```go
	assistantMessage, err := s.chat.AddMessageWithUsage(persistCtx, user.ID, threadID, chat.RoleAssistant, assistantContent, messageUsageFromLLM(assistantResult.Usage))
```

to:

```go
	assistantMessage, err := s.chat.AddMessageWithUsage(persistCtx, user.ID, threadID, chat.RoleAssistant, assistantContent, messageMetricsFromResult(assistantResult))
```

Then replace the `messageUsageFromLLM` function (~line 211-223) with:

```go
func messageMetricsFromResult(result llm.StreamResult) chat.MessageTokenUsage {
	metrics := chat.MessageTokenUsage{}
	if result.Model != "" {
		metrics.Model = strPtr(result.Model)
	}
	if result.Duration > 0 {
		metrics.DurationMs = intPtr(int(result.Duration.Milliseconds()))
	}
	if result.Usage.Present() {
		metrics.PromptTokens = intPtr(result.Usage.PromptTokens)
		metrics.CompletionTokens = intPtr(result.Usage.CompletionTokens)
		metrics.TotalTokens = intPtr(result.Usage.TotalTokens)
		metrics.CachedTokens = intPtr(result.Usage.PromptTokensDetails.CachedTokens)
		metrics.ReasoningTokens = intPtr(result.Usage.CompletionTokenDetails.ReasoningTokens)
	}
	return metrics
}
```

Add a `strPtr` helper next to `intPtr`:

```go
func strPtr(value string) *string {
	return &value
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run <NewTestName>`
Expected: PASS

- [ ] **Step 6: Run full backend test suite**

Run: `cd backend && go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add backend/internal/httpapi/message_stream_handlers.go backend/internal/httpapi/message_stream_handlers_test.go
git commit -m "feat(api): persist generation duration and model on assistant messages"
```

---

## Task 4: Extend the frontend Message type

**Files:**
- Modify: `frontend/src/api.ts` (`Message` type ~line 32-38)

- [ ] **Step 1: Extend the type**

In `frontend/src/api.ts`, replace the `Message` type with:

```ts
export type Message = {
  id: string;
  threadId: string;
  role: "user" | "assistant" | "tool";
  content: string;
  createdAt: string;
  promptTokens?: number;
  completionTokens?: number;
  totalTokens?: number;
  cachedTokens?: number;
  reasoningTokens?: number;
  durationMs?: number;
  model?: string;
};
```

- [ ] **Step 2: Verify the frontend still type-checks**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS (no usages broken; fields are optional).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api.ts
git commit -m "feat(frontend): add token/duration/model fields to Message type"
```

---

## Task 5: Pure metrics formatting helpers

**Files:**
- Create: `frontend/src/metrics.ts`
- Create: `frontend/src/metrics.test.ts`

These helpers are deterministic (no Date/timezone) so they unit-test cleanly. Timestamp formatting lives in the component (Task 6), not here.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/metrics.test.ts`:

```ts
import { expect, test } from "vitest";
import { formatDuration, formatTps, buildMetricsString, hasRenderableMetrics } from "./metrics";
import type { Message } from "./api";

function assistant(extra: Partial<Message>): Message {
  return { id: "m1", threadId: "t1", role: "assistant", content: "hi", createdAt: "2026-05-31T14:32:00Z", ...extra };
}

test("formatDuration scales by magnitude", () => {
  expect(formatDuration(250)).toBe("250ms");
  expect(formatDuration(5200)).toBe("5.2s");
  expect(formatDuration(90000)).toBe("1m 30s");
  expect(formatDuration(3_661_000)).toBe("1h 1m 1s");
});

test("formatTps uses 2 decimals below 1000 and grouping above", () => {
  expect(formatTps(42.125)).toBe("42.13");
  expect(formatTps(1234)).toBe("1 234");
});

test("hasRenderableMetrics requires duration and completion tokens", () => {
  expect(hasRenderableMetrics(assistant({ durationMs: 2000, completionTokens: 100 }))).toBe(true);
  expect(hasRenderableMetrics(assistant({ completionTokens: 100 }))).toBe(false);
  expect(hasRenderableMetrics(assistant({ durationMs: 2000 }))).toBe(false);
  expect(hasRenderableMetrics(assistant({ durationMs: 0, completionTokens: 100 }))).toBe(false);
});

test("buildMetricsString assembles model, duration, tps and token counts", () => {
  const line = buildMetricsString(
    assistant({ model: "mimo", durationMs: 5000, promptTokens: 1234, completionTokens: 500, totalTokens: 1734, cachedTokens: 128, reasoningTokens: 64 }),
  );
  expect(line).toBe("mimo · 5.0s (100.00 tok/s) · 1 234 → 500 (1 734 tok) · cached 128 · reasoning 64");
});

test("buildMetricsString omits absent segments", () => {
  const line = buildMetricsString(assistant({ durationMs: 2000, completionTokens: 100 }));
  expect(line).toBe("2.0s (50.00 tok/s)");
});

test("buildMetricsString returns null without renderable metrics", () => {
  expect(buildMetricsString(assistant({ completionTokens: 100 }))).toBeNull();
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npm run test -- --run src/metrics.test.ts`
Expected: FAIL — cannot resolve `./metrics`.

- [ ] **Step 3: Implement the helpers**

Create `frontend/src/metrics.ts`:

```ts
import type { Message } from "./api";

/** Group integer thousands with a thin spacing (e.g. 1234 -> "1 234"). */
function groupThousands(value: number): string {
  return Math.round(value).toString().replace(/\B(?=(\d{3})+(?!\d))/g, " ");
}

/** Format a duration in milliseconds: ms / s / m s / h m s. */
export function formatDuration(ms: number): string {
  if (ms < 0) return "";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  if (seconds < 3600) {
    const m = Math.floor(seconds / 60);
    const s = Math.floor(seconds % 60);
    return `${m}m ${s}s`;
  }
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  return `${h}h ${m}m ${s}s`;
}

/** Format tokens-per-second: 2 decimals below 1000, grouped above. */
export function formatTps(tps: number): string {
  return tps < 1000 ? tps.toFixed(2) : groupThousands(tps);
}

/** True when there is enough data to show a meaningful tok/s line. */
export function hasRenderableMetrics(message: Message): boolean {
  return Boolean(message.durationMs && message.durationMs > 0 && message.completionTokens);
}

/**
 * Build the metrics line (model · duration (tok/s) · tokens · cached · reasoning),
 * or null when there is nothing renderable. Timestamp is appended by the caller.
 */
export function buildMetricsString(message: Message): string | null {
  if (!hasRenderableMetrics(message)) return null;
  const durationMs = message.durationMs as number;
  const completionTokens = message.completionTokens as number;
  const outputTps = completionTokens / (durationMs / 1000);

  const segments: string[] = [];
  if (message.model) segments.push(message.model);
  segments.push(`${formatDuration(durationMs)} (${formatTps(outputTps)} tok/s)`);
  if (
    message.promptTokens !== undefined &&
    message.completionTokens !== undefined &&
    message.totalTokens !== undefined
  ) {
    segments.push(
      `${groupThousands(message.promptTokens)} → ${groupThousands(message.completionTokens)} (${groupThousands(message.totalTokens)} tok)`,
    );
  }
  if (message.cachedTokens && message.cachedTokens > 0) segments.push(`cached ${groupThousands(message.cachedTokens)}`);
  if (message.reasoningTokens && message.reasoningTokens > 0) segments.push(`reasoning ${groupThousands(message.reasoningTokens)}`);
  return segments.join(" · ");
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npm run test -- --run src/metrics.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/metrics.ts frontend/src/metrics.test.ts
git commit -m "feat(frontend): add pure metrics formatting helpers"
```

---

## Task 6: MessageMetrics component with hover + persisted toggle

**Files:**
- Create: `frontend/src/MessageMetrics.tsx`
- Create: `frontend/src/MessageMetrics.test.tsx`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/MessageMetrics.test.tsx`:

```tsx
import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test } from "vitest";
import { MessageMetrics, MetricsProvider, SHOW_METRICS_KEY } from "./MessageMetrics";
import type { Message } from "./api";

function assistant(extra: Partial<Message>): Message {
  return { id: "m1", threadId: "t1", role: "assistant", content: "hi", createdAt: "2026-05-31T14:32:00Z", ...extra };
}

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  window.localStorage.clear();
});

test("renders nothing without renderable metrics", () => {
  const { container } = render(
    <MetricsProvider>
      <MessageMetrics message={assistant({ completionTokens: 100 })} />
    </MetricsProvider>,
  );
  expect(container).toBeEmptyDOMElement();
});

test("renders the metrics line when data is present", () => {
  render(
    <MetricsProvider>
      <MessageMetrics message={assistant({ model: "mimo", durationMs: 5000, promptTokens: 10, completionTokens: 500, totalTokens: 510 })} />
    </MetricsProvider>,
  );
  expect(screen.getByText(/mimo · 5\.0s \(100\.00 tok\/s\)/)).toBeInTheDocument();
});

test("clicking toggles the persisted always-show preference", () => {
  render(
    <MetricsProvider>
      <MessageMetrics message={assistant({ durationMs: 2000, completionTokens: 100 })} />
    </MetricsProvider>,
  );
  fireEvent.click(screen.getByRole("button"));
  expect(window.localStorage.getItem(SHOW_METRICS_KEY)).toBe("true");
  fireEvent.click(screen.getByRole("button"));
  expect(window.localStorage.getItem(SHOW_METRICS_KEY)).toBe("false");
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npm run test -- --run src/MessageMetrics.test.tsx`
Expected: FAIL — cannot resolve `./MessageMetrics`.

- [ ] **Step 3: Implement the component**

Create `frontend/src/MessageMetrics.tsx`:

```tsx
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import type { Message } from "./api";
import { buildMetricsString } from "./metrics";

export const SHOW_METRICS_KEY = "spark_show_chat_metrics";
const SHOW_METRICS_EVENT = "spark_show_metrics_change";

const MetricsContext = createContext<{
  showAlways: boolean;
  toggle(): void;
}>({ showAlways: false, toggle: () => {} });

function readShowAlways(): boolean {
  return window?.localStorage?.getItem(SHOW_METRICS_KEY) === "true";
}

/** Shares the "always show metrics" preference across every bubble. */
export function MetricsProvider({ children }: { children: ReactNode }) {
  const [showAlways, setShowAlways] = useState(readShowAlways);

  useEffect(() => {
    function handle(event: Event) {
      const detail = (event as CustomEvent<{ showAlways: boolean }>).detail;
      if (detail && typeof detail.showAlways === "boolean") setShowAlways(detail.showAlways);
    }
    window.addEventListener(SHOW_METRICS_EVENT, handle);
    return () => window.removeEventListener(SHOW_METRICS_EVENT, handle);
  }, []);

  function toggle() {
    const next = !readShowAlways();
    window.localStorage.setItem(SHOW_METRICS_KEY, String(next));
    window.dispatchEvent(new CustomEvent(SHOW_METRICS_EVENT, { detail: { showAlways: next } }));
    setShowAlways(next);
  }

  return <MetricsContext.Provider value={{ showAlways, toggle }}>{children}</MetricsContext.Provider>;
}

function formatTimestamp(createdAt: string): string {
  const date = new Date(createdAt);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

/** Renders the hover-only (or always-on) stats line under an assistant bubble. */
export function MessageMetrics({ message }: { message: Message }) {
  const { showAlways, toggle } = useContext(MetricsContext);
  const line = buildMetricsString(message);
  if (line === null) return null;

  const timestamp = formatTimestamp(message.createdAt);
  const full = [line, timestamp].filter(Boolean).join(" · ");

  return (
    <button
      type="button"
      onClick={toggle}
      title={showAlways ? "Click to show metrics only on hover" : "Click to always show metrics"}
      className={`mt-1 block border-none bg-transparent text-left font-mono text-xs text-[#88857d] transition-opacity duration-300 ${
        showAlways ? "opacity-100" : "opacity-0 group-hover:opacity-100"
      }`}
    >
      {full}
    </button>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npm run test -- --run src/MessageMetrics.test.tsx`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/MessageMetrics.tsx frontend/src/MessageMetrics.test.tsx
git commit -m "feat(frontend): MessageMetrics component with hover + persisted toggle"
```

---

## Task 7: Wire MessageMetrics into the transcript

**Files:**
- Modify: `frontend/src/ChatShell.tsx` (imports ~line 4-21; transcript wrapper ~line 732; `MessageBubble` ~line 917; `AssistantText` ~line 944-985)

`AssistantText` is rendered in two places: by `MessageBubble` (has a `Message`) and for live `streamingText` (no message). We add an optional `metricsMessage` prop; only `MessageBubble` passes it. Metrics render in the plain and mixed branches (both already have a `group` wrapper + `MessageActions`); the pure-download branch is intentionally left without metrics (it has no actions row and represents a bare artifact).

- [ ] **Step 1: Import the component**

In `frontend/src/ChatShell.tsx`, add after the `logoImage` import (~line 22):

```ts
import { MessageMetrics, MetricsProvider } from "./MessageMetrics";
```

- [ ] **Step 2: Wrap the transcript list in MetricsProvider**

In `ChatPanel`'s return (~line 732), wrap the inner transcript container. Replace:

```tsx
          <div className="mx-auto w-full max-w-[834px] flex-1 space-y-5">
            {messages.map((message, index) => (
              <MessageBubble
                key={message.id}
                message={message}
                retryContent={message.role === "assistant" ? previousUserContent(messages, index) : null}
```

with:

```tsx
          <MetricsProvider>
          <div className="mx-auto w-full max-w-[834px] flex-1 space-y-5">
            {messages.map((message, index) => (
              <MessageBubble
                key={message.id}
                message={message}
                retryContent={message.role === "assistant" ? previousUserContent(messages, index) : null}
```

Then find the matching closing `</div>` for that container (the one that wraps the `messages.map`, the streaming `AssistantText`, and the thinking indicator) and add `</MetricsProvider>` immediately after it. Confirm by running the dev build's type-check in Step 6; if the JSX is unbalanced, `tsc` will report it.

> To locate the exact closing tag: it is the `</div>` that closes `<div className="mx-auto w-full max-w-[834px] flex-1 space-y-5">`. Read lines ~732-746 of `ChatShell.tsx` before editing to confirm the closing position.

- [ ] **Step 3: Pass the message into AssistantText from MessageBubble**

In `MessageBubble` (~line 917), the assistant return currently reads:

```tsx
  return <AssistantText onRetry={retryContent === null ? undefined : () => onRetry(retryContent)}>{message.content}</AssistantText>;
```

Replace with:

```tsx
  return (
    <AssistantText metricsMessage={message} onRetry={retryContent === null ? undefined : () => onRetry(retryContent)}>
      {message.content}
    </AssistantText>
  );
```

- [ ] **Step 4: Accept and render the prop in AssistantText**

In `AssistantText` (~line 944), change the signature from:

```tsx
function AssistantText({ children, onRetry }: { children: string; onRetry?: () => void }) {
```

to:

```tsx
function AssistantText({
  children,
  onRetry,
  metricsMessage,
}: {
  children: string;
  onRetry?: () => void;
  metricsMessage?: Message;
}) {
```

Then render the metrics after `MessageActions` in the two branches that have it.

In the mixed (download + prose) branch, after its `<MessageActions ... />`, add:

```tsx
        {metricsMessage && <MessageMetrics message={metricsMessage} />}
```

In the final plain branch, after its `<MessageActions ... />`, add:

```tsx
      {metricsMessage && <MessageMetrics message={metricsMessage} />}
```

(The pure-download branch — `before === "" && after === ""` returning `<DownloadResponseBubble />` — is left unchanged by design.)

- [ ] **Step 5: Update existing ChatShell/App tests if they assert transcript structure**

Run: `cd frontend && npm run test -- --run`
Expected: PASS. If any existing test fails because of the added `MetricsProvider` wrapper or metrics text, adjust the test's queries (not the component) so they still target the intended elements. Metrics only render for assistant messages that carry `durationMs` + `completionTokens`, so fixtures without those fields produce no new visible text.

- [ ] **Step 6: Type-check and build**

Run: `cd frontend && npx tsc --noEmit`
Expected: PASS (JSX balanced, prop types correct).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/ChatShell.tsx
git commit -m "feat(frontend): show token stats under assistant bubbles on hover"
```

---

## Task 8: Full verification

- [ ] **Step 1: Backend suite**

Run: `cd backend && go test ./...`
Expected: PASS

- [ ] **Step 2: Frontend suite**

Run: `cd frontend && npm run test -- --run`
Expected: PASS

- [ ] **Step 3: Frontend production build (type-check + bundle)**

Run: `cd frontend && npm run build`
Expected: PASS (`tsc --noEmit && vite build` succeeds).

- [ ] **Step 4: Confirm migration is embedded**

Run: `cd backend && go test ./internal/store/`
Expected: PASS — migrations including `0005_message_metrics.sql` apply in order.

> Note: do not start the full dev stack — the user runs it themselves and will verify the hover UI manually.

---

## Self-Review Notes

- **Spec coverage:** voll (model/duration/tps/prompt→completion/total/cached/reasoning) → Tasks 5-6; DB persistence (migration) → Task 1; duration semantics (final round only) → Tasks 2-3; hover + localStorage toggle + cross-bubble sync → Task 6; render guard for null/historical → `hasRenderableMetrics` in Task 5; all 5 backend touchpoints (migration, StreamResult, handler, store INSERT + both SELECTs, Message struct) → Tasks 1-3.
- **Naming consistency:** `messageMetricsFromResult`, `buildMetricsString`, `hasRenderableMetrics`, `formatDuration` (ms-based), `formatTps`, `MetricsProvider`, `MessageMetrics`, `SHOW_METRICS_KEY = "spark_show_chat_metrics"` used consistently across tasks.
- **Deliberate scope cut:** pure-download artifact bubble shows no metrics (no actions row); documented in Task 7.
