# Lume MiMo Reasoning Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture Xiaomi MiMo `reasoning_content`, preserve it for MiMo multi-turn/tool-call context, stream it to the SPA, and render it in a collapsed/collapsible Thinking panel.

**Architecture:** Keep reasoning structured instead of folding it into assistant `content` with synthetic `<think>` tags. The LLM client parses `delta.reasoning_content` into a separate `ReasoningDelta` stream event and final `StreamResult.ReasoningContent`; HTTP streams it as `assistant_reasoning_delta`; the frontend renders a collapsible panel above the final assistant text. Assistant messages persist `reasoning_content` so future MiMo tool-call turns can include it in history without exposing it as normal assistant text.

**Tech Stack:** Go stdlib HTTP/SSE, SQLite migrations via existing store runner, React + TypeScript + Vitest, Tailwind/theme tokens.

---

## File Structure

- Modify `backend/internal/llm/types.go`: add `reasoning_content` fields to OpenAI-compatible response structs, `llm.Message`, `StreamEvent`, and `StreamResult`.
- Modify `backend/internal/llm/stream.go`: aggregate and emit reasoning deltas independently from content deltas.
- Modify `backend/internal/llm/client_test.go`: cover streamed MiMo reasoning parsing and history serialization.
- Create `backend/internal/store/migrations/0005_message_reasoning_content.sql`: add nullable `messages.reasoning_content`.
- Modify `backend/internal/chat/model.go`, `message_store.go`, `scan.go`, and `store_test.go`: persist and load reasoning content.
- Modify `backend/internal/httpapi/server.go`, `message_stream_handlers.go`, `chat_test_helpers_test.go`, and `message_stream_handlers_test.go`: send `assistant_reasoning_delta`, persist reasoning, and include reasoning in LLM history.
- Modify `frontend/src/api.ts` and `frontend/src/api.test.ts`: add `onReasoningDelta`.
- Modify `frontend/src/ChatShell.tsx` and `frontend/src/App.test.tsx`: maintain streaming reasoning state and render collapsible Thinking UI.
- Modify `frontend/src/index.css`: add minimal Thinking panel styles using existing Lume tokens/colors.

---

### Task 1: Parse MiMo Reasoning In The LLM Client

**Files:**
- Modify: `backend/internal/llm/types.go`
- Modify: `backend/internal/llm/stream.go`
- Test: `backend/internal/llm/client_test.go`

- [ ] **Step 1: Write failing stream parsing test**

Add this test to `backend/internal/llm/client_test.go` after `TestClient_StreamChatResultCapturesUsageTrailerChunk`:

```go
func TestClient_StreamChatResultCapturesReasoningContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"reasoning_content":"I should check "}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"reasoning_content":"the facts."}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"Answer."}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	var events []StreamEvent
	result, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if result.Content != "Answer." {
		t.Fatalf("content = %q, want Answer.", result.Content)
	}
	if result.ReasoningContent != "I should check the facts." {
		t.Fatalf("reasoning content = %q", result.ReasoningContent)
	}
	if len(events) != 3 {
		t.Fatalf("events = %#v, want 3", events)
	}
	if events[0].ReasoningDelta != "I should check " || events[1].ReasoningDelta != "the facts." || events[2].Delta != "Answer." {
		t.Fatalf("events = %#v, want reasoning deltas then content delta", events)
	}
}
```

- [ ] **Step 2: Run the failing backend test**

Run:

```bash
go test ./backend/internal/llm -run TestClient_StreamChatResultCapturesReasoningContent -count=1
```

Expected: FAIL because `StreamResult.ReasoningContent` and `StreamEvent.ReasoningDelta` do not exist.

- [ ] **Step 3: Add structured reasoning types**

In `backend/internal/llm/types.go`, change the relevant structs to include:

```go
type chatCompletionMessage struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
}

type chatCompletionDelta struct {
	Content          string               `json:"content"`
	ReasoningContent string               `json:"reasoning_content"`
	ToolCalls        []ToolCallDeltaChunk `json:"tool_calls"`
}

type StreamEvent struct {
	Delta          string
	ReasoningDelta string
	ToolCall       ToolCall
}

type StreamResult struct {
	Content          string
	ReasoningContent string
	ToolCalls        []ToolCall
	Usage            TokenUsage
}
```

Also extend `Message` in `backend/internal/llm/client.go`:

```go
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}
```

- [ ] **Step 4: Aggregate and emit reasoning deltas**

In `backend/internal/llm/stream.go`, add a `reasoning` builder next to `content`:

```go
var content strings.Builder
var reasoning strings.Builder
```

After `delta := chunk.Choices[0].Delta`, handle reasoning before content:

```go
if delta.ReasoningContent != "" {
	reasoning.WriteString(delta.ReasoningContent)
	if onEvent != nil {
		if err := onEvent(StreamEvent{ReasoningDelta: delta.ReasoningContent}); err != nil {
			logInferenceFailed(ctx, c.model, time.Since(start), err)
			return StreamResult{Content: content.String(), ReasoningContent: reasoning.String(), Usage: usage}, err
		}
	}
}
```

Update all partial error returns in `StreamChatWithTools` to include `ReasoningContent: reasoning.String()`.

Change the `[DONE]` and end-of-stream calls to:

```go
result, err := finishStream(content.String(), reasoning.String(), usage, toolCalls, toolCallOrder, onEvent)
```

Change `finishStream` signature and result initialization:

```go
func finishStream(content string, reasoningContent string, usage TokenUsage, byIndex map[int]*ToolCall, order []int, onEvent func(StreamEvent) error) (StreamResult, error) {
	result := StreamResult{
		Content:          content,
		ReasoningContent: reasoningContent,
		ToolCalls:        make([]ToolCall, 0, len(order)),
		Usage:            usage,
	}
	// existing tool-call loop stays unchanged
	return result, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run:

```bash
go test ./backend/internal/llm -run TestClient_StreamChatResultCapturesReasoningContent -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/llm/types.go backend/internal/llm/client.go backend/internal/llm/stream.go backend/internal/llm/client_test.go
git commit -m "feat: parse MiMo reasoning stream content"
```

---

### Task 2: Persist Assistant Reasoning Content

**Files:**
- Create: `backend/internal/store/migrations/0005_message_reasoning_content.sql`
- Modify: `backend/internal/chat/model.go`
- Modify: `backend/internal/chat/message_store.go`
- Modify: `backend/internal/chat/scan.go`
- Test: `backend/internal/chat/store_test.go`

- [ ] **Step 1: Add failing store test**

Add this test near `TestStore_AddMessageWithUsagePersistsTokenStats` in `backend/internal/chat/store_test.go`:

```go
func TestStore_AddMessageWithUsagePersistsReasoningContent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	userID := createTestUser(t, store, "jan")
	thread := createTestThread(t, store, userID, nil, "Reasoning")

	message, err := store.AddMessageWithUsage(ctx, userID, thread.ID, RoleAssistant, "answer", MessageTokenUsage{
		ReasoningContent: "I checked the inputs first.",
	})
	if err != nil {
		t.Fatalf("AddMessageWithUsage() error: %v", err)
	}
	if message.ReasoningContent != "I checked the inputs first." {
		t.Fatalf("message.ReasoningContent = %q", message.ReasoningContent)
	}

	messages, found, err := store.ListMessages(ctx, userID, thread.ID)
	if err != nil {
		t.Fatalf("ListMessages() error: %v", err)
	}
	if !found || len(messages) != 1 {
		t.Fatalf("messages found=%v len=%d", found, len(messages))
	}
	if messages[0].ReasoningContent != "I checked the inputs first." {
		t.Fatalf("listed reasoning content = %q", messages[0].ReasoningContent)
	}
}
```

- [ ] **Step 2: Run failing store test**

Run:

```bash
go test ./backend/internal/chat -run TestStore_AddMessageWithUsagePersistsReasoningContent -count=1
```

Expected: FAIL because `MessageTokenUsage.ReasoningContent` and `Message.ReasoningContent` do not exist.

- [ ] **Step 3: Add migration**

Create `backend/internal/store/migrations/0005_message_reasoning_content.sql`:

```sql
ALTER TABLE messages ADD COLUMN reasoning_content TEXT NOT NULL DEFAULT '';
```

- [ ] **Step 4: Add model fields**

In `backend/internal/chat/model.go`, add:

```go
type Message struct {
	ID               string          `json:"id"`
	ThreadID         string          `json:"threadId"`
	Role             Role            `json:"role"`
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoningContent,omitempty"`
	ToolCalls        json.RawMessage `json:"toolCalls"`
	Citations        json.RawMessage `json:"citations"`
	PromptTokens     *int            `json:"promptTokens,omitempty"`
	CompletionTokens *int            `json:"completionTokens,omitempty"`
	TotalTokens      *int            `json:"totalTokens,omitempty"`
	CachedTokens     *int            `json:"cachedTokens,omitempty"`
	ReasoningTokens  *int            `json:"reasoningTokens,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
}

type MessageTokenUsage struct {
	PromptTokens      *int
	CompletionTokens  *int
	TotalTokens       *int
	CachedTokens      *int
	ReasoningTokens   *int
	ReasoningContent  string
}
```

- [ ] **Step 5: Persist and scan reasoning**

In `backend/internal/chat/message_store.go`, include `reasoning_content` in the INSERT:

```sql
INSERT INTO messages (
    id,
    thread_id,
    user_id,
    role,
    content,
    reasoning_content,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    cached_tokens,
    reasoning_tokens
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
```

Pass `usage.ReasoningContent` after `content`.

Update both SELECT lists in `ListMessages` and `getMessage`:

```sql
SELECT id, thread_id, role, content, reasoning_content, tool_calls, citations, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, created_at
```

In `backend/internal/chat/scan.go`, scan `reasoning_content` after content:

```go
var toolCalls, citations string
...
&message.Content,
&message.ReasoningContent,
&toolCalls,
&citations,
```

- [ ] **Step 6: Run store test**

Run:

```bash
go test ./backend/internal/chat -run TestStore_AddMessageWithUsagePersistsReasoningContent -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/store/migrations/0005_message_reasoning_content.sql backend/internal/chat/model.go backend/internal/chat/message_store.go backend/internal/chat/scan.go backend/internal/chat/store_test.go
git commit -m "feat: persist assistant reasoning content"
```

---

### Task 3: Stream Reasoning Through HTTP And Preserve It In History

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/message_stream_handlers.go`
- Modify: `backend/internal/httpapi/chat_test_helpers_test.go`
- Test: `backend/internal/httpapi/message_stream_handlers_test.go`

- [ ] **Step 1: Write failing HTTP stream test**

Add this test to `backend/internal/httpapi/message_stream_handlers_test.go`:

```go
func TestStreamMessageSendsAndPersistsReasoningContent(t *testing.T) {
	fakeChat := &fakeChatStore{
		threads: []chat.Thread{{ID: "thr_1", UserID: "usr_1", Title: chat.DefaultThreadTitle}},
	}
	llmClient := fakeChatClient{
		streamText: stringPtr("Answer."),
		reasoningText: "I should reason first.",
	}
	srv := newTestServer(testServerOptions{Chat: fakeChat, LLM: llmClient})

	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: assistant_reasoning_delta") {
		t.Fatalf("body missing assistant_reasoning_delta:\n%s", body)
	}
	if !strings.Contains(body, `"content":"I should reason first."`) {
		t.Fatalf("body missing reasoning content:\n%s", body)
	}
	if len(fakeChat.messages) == 0 {
		t.Fatal("no messages persisted")
	}
	last := fakeChat.messages[len(fakeChat.messages)-1]
	if last.Role != chat.RoleAssistant || last.ReasoningContent != "I should reason first." {
		t.Fatalf("persisted assistant = %#v", last)
	}
}
```

- [ ] **Step 2: Run failing HTTP test**

Run:

```bash
go test ./backend/internal/httpapi -run TestStreamMessageSendsAndPersistsReasoningContent -count=1
```

Expected: FAIL because fake client/store and HTTP handler do not support reasoning yet.

- [ ] **Step 3: Extend HTTP interface and fake store**

In `backend/internal/httpapi/server.go`, keep method names unchanged but the concrete types now carry reasoning in `llm.StreamResult` and `chat.MessageTokenUsage`; no interface method name change is required.

In `backend/internal/httpapi/chat_test_helpers_test.go`, update `fakeChatStore.AddMessageWithUsage` to copy reasoning content into the stored message while keeping the existing deterministic IDs:

```go
message := chat.Message{
	ID:               "msg_1",
	ThreadID:         threadID,
	Role:             role,
	Content:          content,
	ReasoningContent: usage.ReasoningContent,
	CreatedAt:        time.Now().UTC(),
}
if role == chat.RoleAssistant {
	f.assistantContent = content
	f.assistantContextErr = ctx.Err()
	message.ID = "msg_2"
}
```

Add a `reasoningText string` field to `fakeChatClient`:

```go
type fakeChatClient struct {
	title         string
	titleErr      error
	history       *[]llm.Message
	streamText    *string
	reasoningText string
	usage         llm.TokenUsage
	afterStream   func()
}
```

Replace `fakeChatClient.StreamChatWithTools` with this typed event implementation:

```go
func (f fakeChatClient) StreamChatWithTools(_ context.Context, history []llm.Message, _ []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	if f.history != nil {
		*f.history = append((*f.history)[:0], history...)
	}
	if f.reasoningText != "" && onEvent != nil {
		if err := onEvent(llm.StreamEvent{ReasoningDelta: f.reasoningText}); err != nil {
			return llm.StreamResult{}, err
		}
	}
	content := "Hello"
	if f.streamText != nil {
		content = *f.streamText
	}
	if onEvent != nil {
		if f.streamText != nil {
			if err := onEvent(llm.StreamEvent{Delta: content}); err != nil {
				return llm.StreamResult{}, err
			}
		} else {
			for _, delta := range []string{"Hel", "lo"} {
				if err := onEvent(llm.StreamEvent{Delta: delta}); err != nil {
					return llm.StreamResult{}, err
				}
			}
		}
	}
	if f.afterStream != nil {
		f.afterStream()
	}
	return llm.StreamResult{Content: content, ReasoningContent: f.reasoningText, Usage: f.usage}, nil
}
```

- [ ] **Step 4: Stream reasoning events from the handler**

In `backend/internal/httpapi/chat_types.go`, reuse the existing response type for reasoning deltas:

```go
type streamDeltaResponse struct {
	Content string `json:"content"`
}
```

In the no-tool path, replace the `StreamChatResult` callback with `StreamChatWithTools` and a nil tools slice so typed events are available:

```go
return s.llm.StreamChatWithTools(callCtx, history, nil, func(event llm.StreamEvent) error {
	if event.ReasoningDelta != "" {
		return sendSSEJSON(stream, "assistant_reasoning_delta", streamDeltaResponse{Content: event.ReasoningDelta})
	}
	if event.Delta != "" {
		return sendSSEJSON(stream, "assistant_delta", streamDeltaResponse{Content: event.Delta})
	}
	return nil
})
```

In the tool path callback, add reasoning handling before content:

```go
if event.ReasoningDelta != "" {
	return sendSSEJSON(stream, "assistant_reasoning_delta", streamDeltaResponse{Content: event.ReasoningDelta})
}
```

When appending an assistant tool-call turn to `history`, preserve reasoning:

```go
history = append(history, llm.Message{
	Role:             "assistant",
	Content:          result.Content,
	ReasoningContent: result.ReasoningContent,
	ToolCalls:        result.ToolCalls,
})
```

When persisting the final assistant message, pass reasoning through usage:

```go
usage := messageUsageFromLLM(assistantResult.Usage)
usage.ReasoningContent = assistantResult.ReasoningContent
assistantMessage, err := s.chat.AddMessageWithUsage(persistCtx, user.ID, threadID, chat.RoleAssistant, assistantContent, usage)
```

In `buildLLMHistory`, include prior assistant `ReasoningContent` when converting `chat.Message` to `llm.Message`:

```go
history = append(history, llm.Message{
	Role:             string(message.Role),
	Content:          message.Content,
	ReasoningContent: message.ReasoningContent,
})
```

- [ ] **Step 5: Run HTTP test**

Run:

```bash
go test ./backend/internal/httpapi -run TestStreamMessageSendsAndPersistsReasoningContent -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/message_stream_handlers.go backend/internal/httpapi/chat_test_helpers_test.go backend/internal/httpapi/message_stream_handlers_test.go
git commit -m "feat: stream reasoning events to clients"
```

---

### Task 4: Parse Reasoning SSE In The Frontend API

**Files:**
- Modify: `frontend/src/api.ts`
- Test: `frontend/src/api.test.ts`

- [ ] **Step 1: Add failing API parser test**

Add this test to `frontend/src/api.test.ts` after `streamMessage parses server-sent events`:

```ts
test("streamMessage parses assistant reasoning deltas", async () => {
  const body = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: assistant_reasoning_delta\ndata: {"content":"I checked "}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_reasoning_delta\ndata: {"content":"first."}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_delta\ndata: {"content":"Answer."}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status: 200 })));
  const reasoning: string[] = [];
  const deltas: string[] = [];

  await streamMessage("t1", "Hi", {
    onUserMessage: () => undefined,
    onDelta: (delta) => deltas.push(delta),
    onReasoningDelta: (delta) => reasoning.push(delta),
    onAssistantMessage: () => undefined,
    onThread: () => undefined,
  });

  expect(reasoning.join("")).toBe("I checked first.");
  expect(deltas.join("")).toBe("Answer.");
});
```

- [ ] **Step 2: Run failing frontend API test**

Run:

```bash
npm --prefix frontend test -- --run src/api.test.ts -t "assistant reasoning deltas"
```

Expected: FAIL because `onReasoningDelta` is not in `StreamHandlers`.

- [ ] **Step 3: Add handler and event dispatch**

In `frontend/src/api.ts`, update `StreamHandlers`:

```ts
type StreamHandlers = {
  onUserMessage(message: Message): void;
  onDelta(delta: string): void;
  onReasoningDelta?(delta: string): void;
  onAssistantMessage(message: Message): void;
  onThread(thread: Thread): void;
  onToolCall?(event: ToolCallEvent): void;
  onToolResult?(event: ToolResultEvent): void;
  onMcpStatus?(event: McpStatusEvent): void;
};
```

Add a switch case:

```ts
case "assistant_reasoning_delta":
  handlers.onReasoningDelta?.((payload as { content: string }).content);
  break;
```

Extend `Message`:

```ts
export type Message = {
  id: string;
  threadId: string;
  role: "user" | "assistant" | "tool";
  content: string;
  reasoningContent?: string;
  createdAt: string;
};
```

- [ ] **Step 4: Run frontend API test**

Run:

```bash
npm --prefix frontend test -- --run src/api.test.ts -t "assistant reasoning deltas"
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api.ts frontend/src/api.test.ts
git commit -m "feat: parse reasoning stream events"
```

---

### Task 5: Render A Collapsible Thinking Panel

**Files:**
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/index.css`
- Test: `frontend/src/App.test.tsx`

- [ ] **Step 1: Add failing UI test**

Add this test to `frontend/src/App.test.tsx` near the streaming tests:

```tsx
test("renders streamed reasoning in a collapsed thinking panel", async () => {
  vi.stubGlobal(
    "fetch",
    mcpStreamFetch(
      'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n' +
        'event: assistant_reasoning_delta\ndata: {"content":"I checked the source first."}\n\n' +
        'event: assistant_delta\ndata: {"content":"Answer."}\n\n' +
        'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Answer.","reasoningContent":"I checked the source first.","createdAt":"2026-05-30T00:00:01Z"}\n\n' +
        "event: done\ndata: {}\n\n",
    ),
  );

  await sendMessageInExistingChat();

  const toggle = await screen.findByRole("button", { name: /show thinking/i });
  expect(toggle).toBeInTheDocument();
  expect(screen.queryByText("I checked the source first.")).not.toBeInTheDocument();

  fireEvent.click(toggle);

  expect(await screen.findByText("I checked the source first.")).toBeInTheDocument();
  expect(screen.getByText("Answer.")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run failing UI test**

Run:

```bash
npm --prefix frontend test -- --run src/App.test.tsx -t "collapsed thinking panel"
```

Expected: FAIL because the UI ignores `assistant_reasoning_delta`.

- [ ] **Step 3: Track streaming reasoning state**

In `frontend/src/ChatShell.tsx`, add state near `streamingText`:

```tsx
const [streamingReasoning, setStreamingReasoning] = useState("");
```

Reset it on send start and error:

```tsx
setStreamingReasoning("");
```

Add the stream handler:

```tsx
onReasoningDelta: (delta) => {
  if (isCurrentThread()) setStreamingReasoning((current) => current + delta);
},
```

Clear it when the assistant message arrives:

```tsx
setStreamingReasoning("");
```

- [ ] **Step 4: Add collapsible component**

In `frontend/src/ChatShell.tsx`, add this component near `ThinkingIndicator`:

```tsx
function ThinkingPanel({ content, complete }: { content: string; complete: boolean }) {
  const [expanded, setExpanded] = useState(false);
  const trimmed = content.trim();
  if (trimmed === "") return null;
  return (
    <div className="slopr-thinking-panel">
      <button
        aria-expanded={expanded}
        aria-label={expanded ? "Hide thinking" : "Show thinking"}
        className="slopr-thinking-panel-toggle"
        type="button"
        onClick={() => setExpanded((current) => !current)}
      >
        <span>{complete ? "Thinking" : "Thinking..."}</span>
        <span aria-hidden="true" className={expanded ? "slopr-thinking-chevron-expanded" : "slopr-thinking-chevron"}>
          ^
        </span>
      </button>
      {expanded && (
        <div className="slopr-thinking-panel-body">
          <Markdown remarkPlugins={[remarkGfm]}>{trimmed}</Markdown>
        </div>
      )}
    </div>
  );
}
```

Render historical reasoning in `MessageBubble` before assistant content:

```tsx
{message.role === "assistant" && message.reasoningContent && (
  <ThinkingPanel content={message.reasoningContent} complete={true} />
)}
```

Render streaming reasoning before streaming text:

```tsx
{streamingReasoning !== "" && <ThinkingPanel content={streamingReasoning} complete={streamingText !== ""} />}
```

- [ ] **Step 5: Add compact styles**

In `frontend/src/index.css`, add:

```css
.slopr-thinking-panel {
  max-width: 48rem;
  border: 1px solid #3e3d39;
  border-radius: 8px;
  background: #282826;
  color: #aaa79e;
  font-size: 0.8125rem;
}

.slopr-thinking-panel-toggle {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.625rem 0.75rem;
  color: #d8d4ca;
}

.slopr-thinking-chevron,
.slopr-thinking-chevron-expanded {
  display: inline-block;
  line-height: 1;
  transition: transform 0.16s ease;
}

.slopr-thinking-chevron {
  transform: rotate(180deg);
}

.slopr-thinking-chevron-expanded {
  transform: rotate(0deg);
}

.slopr-thinking-panel-body {
  border-top: 1px solid #3e3d39;
  padding: 0.75rem;
  color: #c7c5bd;
}
```

- [ ] **Step 6: Run UI test**

Run:

```bash
npm --prefix frontend test -- --run src/App.test.tsx -t "collapsed thinking panel"
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/ChatShell.tsx frontend/src/index.css frontend/src/App.test.tsx
git commit -m "feat: show collapsible model thinking"
```

---

### Task 6: Full Verification

**Files:**
- Verify all changed backend and frontend files.

- [ ] **Step 1: Run backend tests**

Run:

```bash
make test
```

Expected: PASS.

- [ ] **Step 2: Run frontend tests**

Run:

```bash
make fe-test
```

Expected: PASS.

- [ ] **Step 3: Run frontend build**

Run:

```bash
make fe-build
```

Expected: PASS.

- [ ] **Step 4: Restore tracked embedded frontend placeholders after build**

Run:

```bash
git checkout -- backend/web/dist/.gitkeep backend/web/dist/index.html
```

Expected: `backend/web/dist/.gitkeep` and `backend/web/dist/index.html` are no longer modified by the local build.

- [ ] **Step 5: Run full backend build**

Run:

```bash
make build
```

Expected: PASS and `bin/slopr` exists.

- [ ] **Step 6: Check final diff**

Run:

```bash
git status --short
git diff --check
```

Expected: only intentional source/test/migration changes are listed; `git diff --check` prints no whitespace errors.

- [ ] **Step 7: Commit verification fixes if needed**

If verification required small fixes, commit them:

```bash
git add backend frontend docs/superpowers/plans/2026-05-31-slopr-mimo-reasoning.md
git commit -m "test: verify MiMo reasoning display"
```

If no fixes were needed, do not create an empty commit.

---

## Self-Review

- Spec coverage: MiMo streaming `delta.reasoning_content`, persisted assistant `reasoning_content`, multi-turn/tool-call preservation, SSE event parsing, collapsible frontend display, and verification are covered.
- Placeholder scan: no TBD/TODO/implement-later placeholders remain.
- Type consistency: backend names use `ReasoningContent` / `ReasoningDelta`; API JSON uses `reasoning_content` for MiMo and `reasoningContent` for Lume message JSON; SSE event is `assistant_reasoning_delta`.
