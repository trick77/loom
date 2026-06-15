# Thinking Indicator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Lume's split waiting indicators with one persistent animated `Thinking` block during active assistant turns.

**Architecture:** Keep the change in the existing `ChatShell.tsx` streaming UI. Replace `ThinkingIndicator` with an active `ThinkingPanel` state that can render before reasoning exists, while tool events are running, and while reasoning deltas stream.

**Tech Stack:** React, TypeScript, Vitest, Testing Library, CSS in `frontend/src/index.css`.

---

### Task 1: Active Thinking Block State

**Files:**
- Modify: `frontend/src/App.test.tsx`
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Write failing tests**

Add/update frontend tests so they assert:

```tsx
expect(await screen.findByRole("button", { name: /show thinking/i })).toBeInTheDocument();
expect(screen.getByText("Thinking")).toBeInTheDocument();
expect(screen.queryByRole("status", { name: /slopr is thinking/i })).not.toBeInTheDocument();
```

Add a tool-stream test that sends `user_message -> tool_call` and asserts `Thinking`, the tool name, and `Running` are all visible at the same time.

Add a reasoning-stream test that sends `user_message -> assistant_reasoning_delta` and asserts the same `Thinking` block can be expanded to reveal the reasoning text.

- [ ] **Step 2: Verify tests fail**

Run:

```bash
make fe-test
```

Expected: the new tests fail because the current UI renders dots before reasoning and suppresses the waiting indicator during tool activity.

- [ ] **Step 3: Implement minimal UI state**

In `ChatShell.tsx`, replace `showThinkingIndicator` with `showActiveThinkingPanel`:

```tsx
const showActiveThinkingPanel =
  isSending &&
  streamingText === "" &&
  streamingArtifacts.length === 0 &&
  sendError === "";
```

Render one active `ThinkingPanel` before transient artifacts/text:

```tsx
{showActiveThinkingPanel && (
  <ThinkingPanel content={streamingReasoning} complete={false} active />
)}
```

Move transient `ToolActivityPanel` rendering under that active panel when the assistant turn is still waiting.

- [ ] **Step 4: Add approved animation CSS**

Remove the old dot animation CSS and add a non-repeating `Thinking` shimmer:

```css
.slopr-thinking-label-active {
  position: relative;
  display: inline-block;
  overflow: hidden;
  color: #8f8a81;
  font-weight: 500;
}

.slopr-thinking-label-active::after {
  position: absolute;
  inset: 0;
  content: attr(data-text);
  color: transparent;
  background-image: linear-gradient(90deg, transparent 0%, #f3f0e8 50%, transparent 100%);
  background-position: -62% 0;
  background-repeat: no-repeat;
  background-size: 38% 100%;
  -webkit-background-clip: text;
  background-clip: text;
  opacity: 0;
  animation: slopr-thinking-sweep 4.5s linear infinite;
}
```

- [ ] **Step 5: Verify frontend**

Run:

```bash
make fe-test
```

Expected: all frontend tests pass.

- [ ] **Step 6: Build check**

Run:

```bash
make fe-build
```

Expected: frontend build succeeds. Restore tracked `backend/web/dist` placeholders if Vite rewrites them.
