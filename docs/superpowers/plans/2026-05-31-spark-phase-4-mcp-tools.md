# Spark Phase 4 MCP Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add MCP-backed tool discovery and tool execution to the existing streamed chat path.

**Architecture:** Spark loads `mcp.json` at startup, connects to configured MCP servers, discovers tools, and exposes them to the OpenAI-compatible chat client as function tools. The LLM stream parser detects streamed tool calls, the HTTP handler emits tool status events, Spark executes the requested MCP tools, appends tool-result messages, and resumes streaming until the model returns a final assistant answer.

**Tech Stack:** Go stdlib `net/http`, JSON-RPC 2.0, stdio processes for local MCP servers, OpenAI-compatible chat completions with function tools, existing `net/http` SSE API.

---

### Task 1: MCP Config And Registry

**Files:**
- Create: `backend/internal/mcp/config.go`
- Create: `backend/internal/mcp/config_test.go`

- [ ] **Step 1: Write failing tests**

Cover `mcp.json` parsing for streamable HTTP and stdio servers, including headers/env maps and rejecting duplicate exposed tool names after server-prefix mapping.

- [ ] **Step 2: Run red test**

Run: `cd backend && go test ./internal/mcp`
Expected: package or symbols missing.

- [ ] **Step 3: Implement config types**

Add `Config`, `ServerConfig`, and tool-name mapping helpers. Support `transport` values `streamable-http`, `http`, and `stdio`; treat legacy `http` as remote HTTP for the checked-in config.

- [ ] **Step 4: Run green test**

Run: `cd backend && go test ./internal/mcp`
Expected: pass.

### Task 2: MCP JSON-RPC Clients

**Files:**
- Create: `backend/internal/mcp/client.go`
- Create: `backend/internal/mcp/client_test.go`

- [ ] **Step 1: Write failing tests**

Test remote JSON-RPC calls for `initialize`, `tools/list`, and `tools/call`. Test stdio by running a small Go test helper process that reads JSON-RPC on stdin and writes line-delimited responses on stdout.

- [ ] **Step 2: Run red test**

Run: `cd backend && go test ./internal/mcp`
Expected: missing client implementation.

- [ ] **Step 3: Implement clients**

Implement a `Client` interface with `ListTools`, `CallTool`, and `Close`. Remote clients POST JSON-RPC to the configured endpoint. Stdio clients start the command, write newline-delimited JSON-RPC requests, and read responses until matching IDs.

- [ ] **Step 4: Run green test**

Run: `cd backend && go test ./internal/mcp`
Expected: pass.

### Task 3: OpenAI Tool Schema And Stream Parsing

**Files:**
- Modify: `backend/internal/llm/types.go`
- Modify: `backend/internal/llm/client.go`
- Modify: `backend/internal/llm/stream.go`
- Modify: `backend/internal/llm/client_test.go`

- [ ] **Step 1: Write failing tests**

Assert chat requests include `tools` when provided and the stream parser reconstructs fragmented `tool_calls` deltas while still forwarding normal content deltas.

- [ ] **Step 2: Run red test**

Run: `cd backend && go test ./internal/llm`
Expected: missing tool types or parser support.

- [ ] **Step 3: Implement tool-aware chat**

Add `Tool`, `ToolCall`, `StreamEvent`, and `StreamChatWithTools`. Preserve existing `StreamChat` by delegating to the new method with no tools.

- [ ] **Step 4: Run green test**

Run: `cd backend && go test ./internal/llm`
Expected: pass.

### Task 4: Agent Loop And SSE Events

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/message_stream_handlers.go`
- Modify: `backend/internal/httpapi/chat_test_helpers_test.go`
- Modify: `backend/internal/httpapi/message_stream_handlers_test.go`

- [ ] **Step 1: Write failing tests**

Use a fake chat client that emits a tool call, a fake MCP service that returns text, and assert the handler emits `tool_call` and `tool_result` SSE events, resumes the model stream, and persists the final assistant content.

- [ ] **Step 2: Run red test**

Run: `cd backend && go test ./internal/httpapi -run Tool`
Expected: missing MCP dependency or events.

- [ ] **Step 3: Implement loop**

Inject an MCP service into `httpapi.Deps`. In `handleStreamMessage`, run up to four model/tool rounds, append assistant tool-call messages and tool result messages to the LLM history, emit tool status events, and persist only the final assistant message content for now.

- [ ] **Step 4: Run green test**

Run: `cd backend && go test ./internal/httpapi`
Expected: pass.

### Task 5: Runtime Wiring And Config Examples

**Files:**
- Modify: `backend/cmd/spark/main.go`
- Modify: `mcp.json`
- Modify: `compose.yaml`
- Modify: `.env.example`
- Modify: `README.md`

- [ ] **Step 1: Write failing config/runtime tests if needed**

Add config-loader tests only if new environment variables are introduced. Prefer no new runtime env beyond existing `SPARK_MCP_CONFIG`.

- [ ] **Step 2: Wire startup**

Load `SPARK_MCP_CONFIG` if present, initialize MCP clients, discover tools once at startup, log discovery failures per server, and continue booting with the remaining tools.

- [ ] **Step 3: Update examples**

Update `mcp.json` to use `streamable-http` endpoint names, and document that remote MCP servers should expose a single POST-capable endpoint. Add compose MCP service stubs only for images that are pinned and verified during implementation.

- [ ] **Step 4: Verify**

Run: `make test`, `make fe-test`, `make build`
Expected: all pass; restore `backend/web/dist/.gitkeep` and `backend/web/dist/index.html` if the frontend build overwrites tracked placeholders.

---

## Scope Notes

- Phase 4 does not add RAG citations, document upload, memory, or artifact browsing.
- Generic MCP tool results are shown as tool progress/status events, not source citations.
- Human approval cards remain optional polish unless a concrete sensitive-tool policy is added.
