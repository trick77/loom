# Per-User Usage Accounting + User Menu + Settings/Usage Modal — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persistently count per-user token burn and tool/activity usage that survives chat/project deletion, and surface it in a new user menu → Settings → Usage panel.

**Architecture:** A new `internal/usage` package owns a single-row-per-user counter table (`user_usage_totals`), mutated additively at five increment sites in the httpapi layer. A thin `GET /api/me/usage` endpoint reads the counters plus a live user-memory length. The frontend gets a `UserMenu` popup (Settings · Language · Log out) and a `SettingsModal` whose only nav entry, `UsagePanel`, renders the stats. Counters are best-effort: a write failure logs and never fails the underlying request.

**Tech Stack:** Go (stdlib `database/sql`, SQLite, `net/http`, `slog`), React + TypeScript + Tailwind, Vitest.

**Spec:** `docs/superpowers/specs/2026-06-12-user-usage-accounting-and-menu-design.md`

---

## File Structure

**Backend (create):**
- `backend/internal/store/migrations/0005_user_usage.sql` — counter table
- `backend/internal/usage/store.go` — `Store`, `TokenDelta`, `Totals`, increment + `Get`
- `backend/internal/usage/store_test.go` — store unit tests
- `backend/internal/httpapi/usage.go` — `handleGetUsage`, `usageResponse`

**Backend (modify):**
- `backend/internal/httpapi/server.go` — `UsageStore` iface, `usage` field, `Deps.Usage`, `recordUsage`, route
- `backend/internal/httpapi/tool_dispatch.go` — thread `user`, count search/fetch/obscura/image
- `backend/internal/httpapi/assistant_loop.go` — pass `user` into `executeToolCall`
- `backend/internal/httpapi/message_stream_handler.go` — count tokens after persist
- `backend/internal/httpapi/thread_handlers.go` — count chat created
- `backend/internal/httpapi/project_handlers.go` — count project created
- `backend/cmd/slopr/main.go` — build `usage.NewStore(db)`, pass to `Deps.Usage`
- `backend/internal/httpapi/message_stream_handlers_test.go` — fix 3 `executeToolCall` call sites

**Frontend (create):**
- `ui/src/chat/UserMenu.tsx` — popup menu
- `ui/src/chat/UserMenu.test.tsx`
- `ui/src/settings/SettingsModal.tsx` — modal shell + nav
- `ui/src/settings/UsagePanel.tsx` — stats panel
- `ui/src/settings/UsagePanel.test.tsx`

**Frontend (modify):**
- `ui/src/api.ts` — `Usage` type + `getUsage()`
- `ui/src/chat/ChatShell.tsx` — replace inline Logout with user-row button + menu + modal
- `ui/src/chats/ChatsPage.tsx`, `ui/src/projects/ProjectsPage.tsx`, `ui/src/artifacts/ArtifactsPage.tsx` — `autoFocus` on search inputs

---

## Phase 1 — Backend data layer

### Task 1: Migration for the counter table

**Files:**
- Create: `backend/internal/store/migrations/0005_user_usage.sql`

- [ ] **Step 1: Write the migration**

```sql
CREATE TABLE user_usage_totals (
    user_id           TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens     INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens  INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    web_searches      INTEGER NOT NULL DEFAULT 0,
    web_fetches       INTEGER NOT NULL DEFAULT 0,
    obscura_fetches   INTEGER NOT NULL DEFAULT 0,
    image_gens        INTEGER NOT NULL DEFAULT 0,
    chats_created     INTEGER NOT NULL DEFAULT 0,
    projects_created  INTEGER NOT NULL DEFAULT 0,
    updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);
```

- [ ] **Step 2: Verify it applies**

Run: `cd backend && go test ./internal/store/...`
Expected: PASS (migrations run on `store.Open`; existing store tests exercise it).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/store/migrations/0005_user_usage.sql
git commit -m "feat(usage): add user_usage_totals table"
```

### Task 2: usage.Store with token + counter writes and Get

**Files:**
- Create: `backend/internal/usage/store.go`
- Test: `backend/internal/usage/store_test.go`

- [ ] **Step 1: Write the failing test**

```go
package usage

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE user_usage_totals (
		user_id TEXT PRIMARY KEY,
		prompt_tokens INTEGER NOT NULL DEFAULT 0,
		completion_tokens INTEGER NOT NULL DEFAULT 0,
		cached_tokens INTEGER NOT NULL DEFAULT 0,
		reasoning_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		web_searches INTEGER NOT NULL DEFAULT 0,
		web_fetches INTEGER NOT NULL DEFAULT 0,
		obscura_fetches INTEGER NOT NULL DEFAULT 0,
		image_gens INTEGER NOT NULL DEFAULT 0,
		chats_created INTEGER NOT NULL DEFAULT 0,
		projects_created INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return NewStore(db)
}

func TestGet_unknownUser_returnsZeroTotals(t *testing.T) {
	store := newTestStore(t)
	got, err := store.Get(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != (Totals{}) {
		t.Fatalf("want zero Totals, got %+v", got)
	}
}

func TestCounters_areAdditiveAndCreateRow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.AddTokens(ctx, "u1", TokenDelta{PromptTokens: 10, CompletionTokens: 5, CachedTokens: 2, ReasoningTokens: 3, TotalTokens: 18}); err != nil {
		t.Fatalf("AddTokens: %v", err)
	}
	if err := store.AddTokens(ctx, "u1", TokenDelta{PromptTokens: 1, TotalTokens: 1}); err != nil {
		t.Fatalf("AddTokens 2: %v", err)
	}
	for _, inc := range []func(context.Context, string) error{
		store.IncWebSearch, store.IncWebFetch, store.IncObscuraFetch,
		store.IncImageGen, store.IncChatCreated, store.IncProjectCreated,
	} {
		if err := inc(ctx, "u1"); err != nil {
			t.Fatalf("inc: %v", err)
		}
	}
	got, err := store.Get(ctx, "u1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := Totals{
		PromptTokens: 11, CompletionTokens: 5, CachedTokens: 2, ReasoningTokens: 3, TotalTokens: 19,
		WebSearches: 1, WebFetches: 1, ObscuraFetches: 1, ImageGens: 1, ChatsCreated: 1, ProjectsCreated: 1,
	}
	if got != want {
		t.Fatalf("totals mismatch:\n got %+v\nwant %+v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/usage/...`
Expected: FAIL — `undefined: NewStore` / `Store` / `Totals` / `TokenDelta`.

- [ ] **Step 3: Write the implementation**

```go
// Package usage owns per-user lifetime usage counters. The totals live in one
// row per user (user_usage_totals) and are mutated additively, so they survive
// deletion of the chats/projects that produced them. All writes are best-effort
// from the caller's perspective: callers log and swallow errors so a counter
// failure never fails the underlying request.
package usage

import (
	"context"
	"database/sql"
	"errors"
)

// DBTX is the minimal database surface the store needs (satisfied by *sql.DB).
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// TokenDelta is one turn's token usage to add to a user's lifetime totals.
type TokenDelta struct {
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	ReasoningTokens  int
	TotalTokens      int
}

// Totals is a user's lifetime usage. JSON tags match the frontend Usage type.
type Totals struct {
	PromptTokens     int `json:"promptTokens"`
	CompletionTokens int `json:"completionTokens"`
	CachedTokens     int `json:"cachedTokens"`
	ReasoningTokens  int `json:"reasoningTokens"`
	TotalTokens      int `json:"totalTokens"`
	WebSearches      int `json:"webSearches"`
	WebFetches       int `json:"webFetches"`
	ObscuraFetches   int `json:"obscuraFetches"`
	ImageGens        int `json:"imageGens"`
	ChatsCreated     int `json:"chatsCreated"`
	ProjectsCreated  int `json:"projectsCreated"`
}

type Store struct{ db DBTX }

func NewStore(db DBTX) *Store { return &Store{db: db} }

// AddTokens adds one turn's token usage to the user's lifetime totals, creating
// the row on first use.
func (s *Store) AddTokens(ctx context.Context, userID string, d TokenDelta) error {
	const q = `INSERT INTO user_usage_totals
		(user_id, prompt_tokens, completion_tokens, cached_tokens, reasoning_tokens, total_tokens)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			prompt_tokens     = prompt_tokens + excluded.prompt_tokens,
			completion_tokens = completion_tokens + excluded.completion_tokens,
			cached_tokens     = cached_tokens + excluded.cached_tokens,
			reasoning_tokens  = reasoning_tokens + excluded.reasoning_tokens,
			total_tokens      = total_tokens + excluded.total_tokens,
			updated_at        = datetime('now')`
	_, err := s.db.ExecContext(ctx, q, userID,
		d.PromptTokens, d.CompletionTokens, d.CachedTokens, d.ReasoningTokens, d.TotalTokens)
	return err
}

func (s *Store) IncWebSearch(ctx context.Context, userID string) error    { return s.bump(ctx, userID, "web_searches") }
func (s *Store) IncWebFetch(ctx context.Context, userID string) error     { return s.bump(ctx, userID, "web_fetches") }
func (s *Store) IncObscuraFetch(ctx context.Context, userID string) error { return s.bump(ctx, userID, "obscura_fetches") }
func (s *Store) IncImageGen(ctx context.Context, userID string) error     { return s.bump(ctx, userID, "image_gens") }
func (s *Store) IncChatCreated(ctx context.Context, userID string) error  { return s.bump(ctx, userID, "chats_created") }
func (s *Store) IncProjectCreated(ctx context.Context, userID string) error {
	return s.bump(ctx, userID, "projects_created")
}

// bump adds 1 to a single counter column. column is always a compile-time
// constant from this package — never user input — so string interpolation here
// is safe from injection.
func (s *Store) bump(ctx context.Context, userID, column string) error {
	q := "INSERT INTO user_usage_totals (user_id, " + column + ") VALUES (?, 1) " +
		"ON CONFLICT(user_id) DO UPDATE SET " + column + " = " + column + " + 1, updated_at = datetime('now')"
	_, err := s.db.ExecContext(ctx, q, userID)
	return err
}

// Get returns the user's lifetime totals, or a zero Totals if no row exists yet.
func (s *Store) Get(ctx context.Context, userID string) (Totals, error) {
	const q = `SELECT prompt_tokens, completion_tokens, cached_tokens, reasoning_tokens, total_tokens,
		web_searches, web_fetches, obscura_fetches, image_gens, chats_created, projects_created
		FROM user_usage_totals WHERE user_id = ?`
	var t Totals
	err := s.db.QueryRowContext(ctx, q, userID).Scan(
		&t.PromptTokens, &t.CompletionTokens, &t.CachedTokens, &t.ReasoningTokens, &t.TotalTokens,
		&t.WebSearches, &t.WebFetches, &t.ObscuraFetches, &t.ImageGens, &t.ChatsCreated, &t.ProjectsCreated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Totals{}, nil
	}
	if err != nil {
		return Totals{}, err
	}
	return t, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/usage/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/usage/
git commit -m "feat(usage): add per-user usage store"
```

---

## Phase 2 — Backend wiring

### Task 3: Server interface, field, Deps, recordUsage helper, route

**Files:**
- Modify: `backend/internal/httpapi/server.go`

- [ ] **Step 1: Add the `UsageStore` interface** (place near the other store interfaces, after `ChatStore`)

```go
// UsageStore records per-user lifetime usage counters. All methods are
// best-effort from the caller's side; see server.recordUsage.
type UsageStore interface {
	AddTokens(context.Context, string, usage.TokenDelta) error
	IncWebSearch(context.Context, string) error
	IncWebFetch(context.Context, string) error
	IncObscuraFetch(context.Context, string) error
	IncImageGen(context.Context, string) error
	IncChatCreated(context.Context, string) error
	IncProjectCreated(context.Context, string) error
	Get(context.Context, string) (usage.Totals, error)
}
```

Add the import `"github.com/trick77/lume/internal/usage"` to the import block.

- [ ] **Step 2: Add `Usage` to `Deps` and `usage` to `server`**

In `Deps` (after `Chat ChatStore`):
```go
	Usage                 UsageStore
```
In `server` struct (after `chat ChatStore`):
```go
	usage                 UsageStore
```
In `func New(d Deps)` mapping (after `chat: d.Chat,`):
```go
		usage:                 d.Usage,
```

- [ ] **Step 3: Add the `recordUsage` helper** (near other server helpers in server.go)

```go
// recordUsage runs a best-effort usage-counter update. A nil store (e.g. in
// tests) or any write error is logged and swallowed so counting never fails the
// underlying request. counter is a short label used only for logging.
func (s *server) recordUsage(counter string, fn func() error) {
	if s.usage == nil {
		return
	}
	if err := fn(); err != nil {
		slog.Warn("usage counter update failed", "counter", counter, "error", err)
	}
}
```

Confirm `log/slog` is already imported in server.go (it is used elsewhere); if not, add it.

- [ ] **Step 4: Register the route** (in the mux setup, next to `GET /api/me/memory`)

```go
	mux.Handle("GET /api/me/usage", s.requireAuth(http.HandlerFunc(s.handleGetUsage)))
```

- [ ] **Step 5: Verify it compiles** (handler added in Task 7; until then this references an undefined method — so defer build to Task 7). For now:

Run: `cd backend && go vet ./internal/httpapi/ 2>&1 | head`
Expected: only an `undefined: s.handleGetUsage` error remains (resolved in Task 7). No other errors.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/server.go
git commit -m "feat(usage): wire UsageStore into httpapi server"
```

### Task 4: Construct the store in main.go

**Files:**
- Modify: `backend/cmd/slopr/main.go`

- [ ] **Step 1: Build the store** (after `artifactStore := artifact.NewStore(db)`, ~line 100)

```go
	usageStore := usage.NewStore(db)
```

Add import `"github.com/trick77/lume/internal/usage"`.

- [ ] **Step 2: Pass it to Deps** (in the `httpapi.New(httpapi.Deps{...})` literal, after `Artifacts: artifactStore,`)

```go
		Usage:                 usageStore,
```

- [ ] **Step 3: Build** (will still fail on `handleGetUsage` until Task 7 — that is expected). Defer full build to Task 7.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/slopr/main.go
git commit -m "feat(usage): construct usage store in main"
```

---

## Phase 3 — Backend increment sites

### Task 5: Thread `user` into executeToolCall and count search/fetch/obscura

**Files:**
- Modify: `backend/internal/httpapi/tool_dispatch.go`
- Modify: `backend/internal/httpapi/assistant_loop.go:89`
- Modify: `backend/internal/httpapi/message_stream_handlers_test.go` (3 call sites)

- [ ] **Step 1: Add the exposed Tavily tool-name constant** (in the `const (...)` block beside `fetchToolName` in tool_dispatch.go)

```go
	// tavilySearchExposedName is the namespaced web-search tool as dispatched
	// (server "tavily" + tool "tavily_search"). See internal/mcp ExposedToolName.
	tavilySearchExposedName = "tavily__tavily_search"
```

- [ ] **Step 2: Change `executeToolCall` to take `user` and count on success**

Replace the signature and success path:
```go
func (s *server) executeToolCall(ctx context.Context, user auth.User, call llm.ToolCall, round int) string {
	args := summarizeForLog(call.Function.Arguments)
	arguments, err := parseToolArguments(call.Function.Arguments)
	if err != nil {
		slog.Warn("tool call rejected: invalid arguments", "tool", call.Function.Name, "round", round, "args", args, "err", err)
		return capToolOutput("tool failed: invalid arguments: " + err.Error())
	}
	callCtx, cancel := context.WithTimeout(ctx, maxToolCallDuration)
	defer cancel()
	start := time.Now()
	output, err := s.mcp.CallTool(callCtx, call.Function.Name, arguments)
	durationMS := time.Since(start).Milliseconds()
	if err != nil {
		slog.Warn("tool call failed", "tool", call.Function.Name, "round", round, "args", args, "duration_ms", durationMS, "err", err)
		if fallback, ok := s.fetchObscuraFallback(callCtx, user, call.Function.Name, arguments, round); ok {
			return fallback
		}
		return capToolOutput("tool failed: " + err.Error())
	}
	slog.Info("tool call completed", "tool", call.Function.Name, "round", round, "args", args, "duration_ms", durationMS, "result_bytes", len(output))
	s.countToolCall(ctx, user, call.Function.Name)
	return capToolOutput(output)
}

// countToolCall increments the per-user counter for a successfully completed
// tool call. Only web search and the lightweight fetch are counted here; the
// fetch->obscura fallback counts obscura separately in fetchObscuraFallback.
func (s *server) countToolCall(ctx context.Context, user auth.User, toolName string) {
	switch toolName {
	case tavilySearchExposedName:
		s.recordUsage("web_search", func() error { return s.usage.IncWebSearch(ctx, user.ID) })
	case fetchToolName:
		s.recordUsage("web_fetch", func() error { return s.usage.IncWebFetch(ctx, user.ID) })
	}
}
```

- [ ] **Step 3: Add `user` to `fetchObscuraFallback` and count obscura on success**

Change its signature and the success path:
```go
func (s *server) fetchObscuraFallback(ctx context.Context, user auth.User, toolName string, arguments map[string]any, round int) (string, bool) {
	if toolName != fetchToolName {
		return "", false
	}
	if !s.mcp.HasTool(obscuraNavigateToolName) || !s.mcp.HasTool(obscuraSnapshotToolName) {
		return "", false
	}
	url, ok := arguments["url"].(string)
	if !ok || strings.TrimSpace(url) == "" {
		return "", false
	}
	if _, err := s.mcp.CallTool(ctx, obscuraNavigateToolName, map[string]any{"url": url}); err != nil {
		slog.Warn("obscura fallback navigate failed", "url", url, "round", round, "err", err)
		return "", false
	}
	snapshot, err := s.mcp.CallTool(ctx, obscuraSnapshotToolName, map[string]any{})
	if err != nil {
		slog.Warn("obscura fallback snapshot failed", "url", url, "round", round, "err", err)
		return "", false
	}
	slog.Info("fetch failed, obscura fallback succeeded", "url", url, "round", round, "result_bytes", len(snapshot))
	s.recordUsage("obscura_fetch", func() error { return s.usage.IncObscuraFetch(ctx, user.ID) })
	return capToolOutput(snapshot), true
}
```

- [ ] **Step 4: Update the caller in assistant_loop.go:89**

```go
				output = s.executeToolCall(ctx, user, call, round)
```
(`user` is already in scope at that point — `executeBuiltInTool(ctx, stream, user, thread, call)` is called just above.)

- [ ] **Step 5: Update the 3 test call sites** in `message_stream_handlers_test.go` (lines ~754, 764, 787)

Each currently reads `srv.executeToolCall(context.Background(), <call>, 0)`. Change to pass a user:
```go
		got := srv.executeToolCall(context.Background(), auth.User{ID: "u1", Username: "u1"}, fetchCall, 0)
```
Apply the same `auth.User{ID: "u1", Username: "u1"}` arg to all three (`fetchCall`, `fetchCall`, `otherCall`). Ensure `"github.com/trick77/lume/internal/auth"` is imported in the test file (it almost certainly already is; add if missing).

- [ ] **Step 6: Build & run package tests**

Run: `cd backend && go build ./... 2>&1 | grep -v handleGetUsage; go test ./internal/httpapi/ -run TestExecuteToolCall -v 2>&1 | tail -20`
Expected: builds except the still-missing `handleGetUsage` (Task 7); the existing tool-call tests pass with the new signature.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/httpapi/tool_dispatch.go backend/internal/httpapi/assistant_loop.go backend/internal/httpapi/message_stream_handlers_test.go
git commit -m "feat(usage): count web search, fetch, and obscura fallback per user"
```

### Task 6: Count image gen, token burn, chats, projects

**Files:**
- Modify: `backend/internal/httpapi/tool_dispatch.go` (`executeImageTool`)
- Modify: `backend/internal/httpapi/message_stream_handler.go`
- Modify: `backend/internal/httpapi/thread_handlers.go:60`
- Modify: `backend/internal/httpapi/project_handlers.go:37`

- [ ] **Step 1: Count image gen** — in `executeImageTool`, immediately before the final success return (`return &response, fmt.Sprintf(...), true`):

```go
	s.recordUsage("image_gen", func() error { return s.usage.IncImageGen(ctx, user.ID) })
	_ = sendSSEJSON(stream, "artifact", response)
	return &response, fmt.Sprintf("created image artifact %s (%d bytes)", response.DisplayFilename, response.SizeBytes), true
```
(Insert the `recordUsage` line directly above the existing `sendSSEJSON`/return; `user` and `ctx` are already parameters.)

- [ ] **Step 2: Count token burn** — in `message_stream_handler.go`, right after the assistant message is persisted (after `assistantMessage, err := s.chat.AddMessageWithActivityTrace(...)` and its error check, before `sendSSEJSON(stream, "assistant_message", ...)`):

```go
	turnUsage := usageTotal.Total()
	s.recordUsage("tokens", func() error {
		return s.usage.AddTokens(persistCtx, user.ID, usage.TokenDelta{
			PromptTokens:     turnUsage.PromptTokens,
			CompletionTokens: turnUsage.CompletionTokens,
			CachedTokens:     turnUsage.PromptTokensDetails.CachedTokens,
			ReasoningTokens:  turnUsage.CompletionTokenDetails.ReasoningTokens,
			TotalTokens:      turnUsage.TotalTokens,
		})
	})
```
This reads the accumulator after `titles.wait()` (already called above persist), so answer + tool-round + title + reasoning-abstract tokens are all included, matching the per-message stats. Add import `"github.com/trick77/lume/internal/usage"` to this file.

- [ ] **Step 3: Count chats created** — in `thread_handlers.go`, after the `CreateThread` success check (after the error from `s.chat.CreateThread(...)` is confirmed nil):

```go
	s.recordUsage("chat_created", func() error { return s.usage.IncChatCreated(r.Context(), user.ID) })
```
Place it after the existing error handling for `CreateThread`, where `thread` is known good and `user` is in scope.

- [ ] **Step 4: Count projects created** — in `project_handlers.go`, after the `CreateProject` success check:

```go
	s.recordUsage("project_created", func() error { return s.usage.IncProjectCreated(r.Context(), user.ID) })
```

- [ ] **Step 5: Build**

Run: `cd backend && go build ./... 2>&1 | grep -v handleGetUsage`
Expected: no errors except the pending `handleGetUsage` (Task 7).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/tool_dispatch.go backend/internal/httpapi/message_stream_handler.go backend/internal/httpapi/thread_handlers.go backend/internal/httpapi/project_handlers.go
git commit -m "feat(usage): count image gen, tokens, chats, and projects per user"
```

---

## Phase 4 — Backend endpoint

### Task 7: GET /api/me/usage

**Files:**
- Create: `backend/internal/httpapi/usage.go`
- Test: `backend/internal/httpapi/usage_test.go`

- [ ] **Step 1: Write the failing test**

```go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/lume/internal/auth"
	"github.com/trick77/lume/internal/usage"
)

type stubUsage struct{ totals usage.Totals }

func (s stubUsage) AddTokens(context.Context, string, usage.TokenDelta) error { return nil }
func (s stubUsage) IncWebSearch(context.Context, string) error               { return nil }
func (s stubUsage) IncWebFetch(context.Context, string) error               { return nil }
func (s stubUsage) IncObscuraFetch(context.Context, string) error           { return nil }
func (s stubUsage) IncImageGen(context.Context, string) error               { return nil }
func (s stubUsage) IncChatCreated(context.Context, string) error            { return nil }
func (s stubUsage) IncProjectCreated(context.Context, string) error         { return nil }
func (s stubUsage) Get(context.Context, string) (usage.Totals, error)       { return s.totals, nil }

func TestHandleGetUsage_returnsTotalsAndMemoryLength(t *testing.T) {
	srv := &server{usage: stubUsage{totals: usage.Totals{TotalTokens: 42, WebSearches: 3}}, chat: newMemoryChatStoreForUsageTest(t)}
	req := httptest.NewRequest(http.MethodGet, "/api/me/usage", nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), auth.User{ID: "u1", Username: "u1"}))
	rec := httptest.NewRecorder()

	srv.handleGetUsage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got usageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TotalTokens != 42 || got.WebSearches != 3 {
		t.Fatalf("totals not surfaced: %+v", got)
	}
	if got.UserMemoryMax != 2000 {
		t.Fatalf("UserMemoryMax = %d, want 2000", got.UserMemoryMax)
	}
}
```

NOTE for the implementer: reuse the existing chat-store test helper used by the other httpapi handler tests (look at how `thread_handlers_test.go` builds its `chatStore`) for `newMemoryChatStoreForUsageTest`. Confirm the helper for injecting a user into context — match whatever `auth` helper the other httpapi tests use (`auth.ContextWithUser` or equivalent; grep `WithContext` / `ContextWithUser` in existing `*_test.go`). Adjust the two helper calls to the established pattern rather than inventing new ones.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestHandleGetUsage`
Expected: FAIL — `undefined: server.handleGetUsage` / `usageResponse`.

- [ ] **Step 3: Write the handler**

```go
package httpapi

import (
	"net/http"

	"github.com/trick77/lume/internal/auth"
	"github.com/trick77/lume/internal/chat"
	"github.com/trick77/lume/internal/usage"
)

// usageResponse is the GET /api/me/usage payload: the user's lifetime counters
// plus the live (non-counter) current user-memory length.
type usageResponse struct {
	usage.Totals
	UserMemoryLength int `json:"userMemoryLength"`
	UserMemoryMax    int `json:"userMemoryMax"`
}

func (s *server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var totals usage.Totals
	if s.usage != nil {
		t, err := s.usage.Get(r.Context(), user.ID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "load usage failed")
			return
		}
		totals = t
	}
	// Live value, not a counter: current length of the user's memory in runes.
	memLen := 0
	if mem, _, err := s.chat.GetUserMemory(r.Context(), user.ID); err == nil {
		memLen = len([]rune(mem.Content))
	}
	writeJSON(w, usageResponse{Totals: totals, UserMemoryLength: memLen, UserMemoryMax: chat.MaxUserMemoryLength})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestHandleGetUsage -v`
Expected: PASS.

- [ ] **Step 5: Full backend build + test**

Run: `cd backend && go build ./... && go test ./...`
Expected: builds clean; all tests pass.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/usage.go backend/internal/httpapi/usage_test.go
git commit -m "feat(usage): add GET /api/me/usage endpoint"
```

---

## Phase 5 — Frontend API

### Task 8: Usage type + getUsage()

**Files:**
- Modify: `ui/src/api.ts`

- [ ] **Step 1: Add the type** (near the other exported types, e.g. after `UserMemory`)

```ts
export type Usage = {
  promptTokens: number;
  completionTokens: number;
  cachedTokens: number;
  reasoningTokens: number;
  totalTokens: number;
  webSearches: number;
  webFetches: number;
  obscuraFetches: number;
  imageGens: number;
  chatsCreated: number;
  projectsCreated: number;
  userMemoryLength: number;
  userMemoryMax: number;
};
```

- [ ] **Step 2: Add the fetch function** (next to `getUserMemory`)

```ts
export async function getUsage(): Promise<Usage> {
  const response = await fetch(`/api/me/usage`);
  return expectJSON<Usage>(response, "failed to load usage");
}
```

- [ ] **Step 3: Typecheck**

Run: `cd ui && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add ui/src/api.ts
git commit -m "feat(usage): add getUsage API client"
```

---

## Phase 6 — Frontend user menu

### Task 9: UserMenu component

**Files:**
- Create: `ui/src/chat/UserMenu.tsx`
- Test: `ui/src/chat/UserMenu.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { UserMenu } from "./UserMenu";

describe("UserMenu", () => {
  it("renders Settings, Language and Log out and fires callbacks", async () => {
    const onSettings = vi.fn();
    const onLogout = vi.fn();
    render(<UserMenu onSettings={onSettings} onLogout={onLogout} onClose={() => {}} />);

    expect(screen.getByRole("menuitem", { name: "Settings" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Language" })).toBeInTheDocument();

    await userEvent.click(screen.getByRole("menuitem", { name: "Settings" }));
    expect(onSettings).toHaveBeenCalledOnce();

    await userEvent.click(screen.getByRole("menuitem", { name: "Log out" }));
    expect(onLogout).toHaveBeenCalledOnce();
  });

  it("Language is inert (no callback prop, does not throw)", async () => {
    render(<UserMenu onSettings={() => {}} onLogout={() => {}} onClose={() => {}} />);
    await userEvent.click(screen.getByRole("menuitem", { name: "Language" }));
    // No assertion beyond "did not throw" — Language is a dead entry for now.
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ui && npx vitest run src/chat/UserMenu.test.tsx`
Expected: FAIL — cannot resolve `./UserMenu`.

- [ ] **Step 3: Write the component** (mirrors `ThreadActionsMenu` styling/structure)

```tsx
import { Icon } from "./Icon";

/**
 * UserMenu — popup opened from the sidebar user row. Settings opens the settings
 * modal; Language is a deliberate dead entry for now (wired later); Log out runs
 * the existing logout. Styling mirrors ThreadActionsMenu.
 */
export function UserMenu({
  onSettings,
  onLogout,
  onClose,
  className = "bottom-full left-0 mb-2",
}: {
  onSettings(): void;
  onLogout(): void;
  onClose(): void;
  className?: string;
}) {
  return (
    <div
      aria-label="User menu"
      className={`ui-sidebar-text absolute z-30 w-[220px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${className}`}
      role="menu"
    >
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8] hover:bg-[#3f3f3a]"
        role="menuitem"
        type="button"
        onClick={() => {
          onClose();
          onSettings();
        }}
      >
        <Icon name="settings" size="19px" className="grid h-[21px] w-[21px] shrink-0 place-items-center" />
        Settings
      </button>
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8] hover:bg-[#3f3f3a]"
        role="menuitem"
        type="button"
        onClick={() => {
          /* Language switching is not wired yet — deliberate dead entry. */
        }}
      >
        <Icon name="globe" size="19px" className="grid h-[21px] w-[21px] shrink-0 place-items-center" />
        Language
      </button>
      <div className="mx-[14px] my-[5px] h-px bg-[#4a4741]" role="separator" />
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8] hover:bg-[#3f3f3a]"
        role="menuitem"
        type="button"
        onClick={() => {
          onClose();
          onLogout();
        }}
      >
        <LogoutMenuIcon />
        Log out
      </button>
    </div>
  );
}

function LogoutMenuIcon() {
  return (
    <svg className="h-[21px] w-[21px] shrink-0" viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path d="M14 7V5.5C14 4.7 13.3 4 12.5 4H6C5.2 4 4.5 4.7 4.5 5.5v13c0 .8.7 1.5 1.5 1.5h6.5c.8 0 1.5-.7 1.5-1.5V17" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M10 12h10m0 0-3-3m3 3-3 3" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
```

NOTE: Settings uses the font `settings` glyph and Language the `globe` glyph (both exist in `Icon.tsx`). Log out uses an inline SVG matching the file's existing inline-icon pattern (e.g. `ArchiveMenuIcon`). If the exact icon-font logout glyph is preferred later, find its codepoint via the icon specimen (serve `public/icons.html` over HTTP — `file://` is blocked) and swap `LogoutMenuIcon` for `<Icon name="logout" .../>`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ui && npx vitest run src/chat/UserMenu.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/src/chat/UserMenu.tsx ui/src/chat/UserMenu.test.tsx
git commit -m "feat(usage): add UserMenu popup component"
```

---

## Phase 7 — Frontend settings modal + usage panel

### Task 10: UsagePanel

**Files:**
- Create: `ui/src/settings/UsagePanel.tsx`
- Test: `ui/src/settings/UsagePanel.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { UsagePanel } from "./UsagePanel";
import * as api from "../api";

const sample: api.Usage = {
  promptTokens: 100, completionTokens: 50, cachedTokens: 10, reasoningTokens: 5, totalTokens: 150,
  webSearches: 4, webFetches: 7, obscuraFetches: 2, imageGens: 1, chatsCreated: 9, projectsCreated: 3,
  userMemoryLength: 1234, userMemoryMax: 2000,
};

describe("UsagePanel", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("renders counters and memory length from the API", async () => {
    vi.spyOn(api, "getUsage").mockResolvedValue(sample);
    render(<UsagePanel />);
    expect(await screen.findByText("150")).toBeInTheDocument(); // total tokens
    expect(screen.getByText("4")).toBeInTheDocument(); // web searches
    expect(screen.getByText("1234 / 2000")).toBeInTheDocument(); // memory length
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ui && npx vitest run src/settings/UsagePanel.test.tsx`
Expected: FAIL — cannot resolve `./UsagePanel`.

- [ ] **Step 3: Write the component**

```tsx
import { useEffect, useState } from "react";
import { getUsage, type Usage } from "../api";

type Row = { label: string; value: string };

function rowsFor(u: Usage): { group: string; rows: Row[] }[] {
  return [
    {
      group: "Tokens",
      rows: [
        { label: "Total", value: String(u.totalTokens) },
        { label: "Prompt", value: String(u.promptTokens) },
        { label: "Completion", value: String(u.completionTokens) },
        { label: "Cached", value: String(u.cachedTokens) },
        { label: "Reasoning", value: String(u.reasoningTokens) },
      ],
    },
    {
      group: "Tools",
      rows: [
        { label: "Web searches", value: String(u.webSearches) },
        { label: "Web fetches", value: String(u.webFetches) },
        { label: "Obscura fetches", value: String(u.obscuraFetches) },
        { label: "Image generations", value: String(u.imageGens) },
      ],
    },
    {
      group: "Activity",
      rows: [
        { label: "Chats created", value: String(u.chatsCreated) },
        { label: "Projects created", value: String(u.projectsCreated) },
      ],
    },
    {
      group: "Memory",
      rows: [{ label: "User memory length", value: `${u.userMemoryLength} / ${u.userMemoryMax}` }],
    },
  ];
}

export function UsagePanel() {
  const [usage, setUsage] = useState<Usage | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    getUsage()
      .then((u) => active && setUsage(u))
      .catch(() => active && setError("Failed to load usage."));
    return () => {
      active = false;
    };
  }, []);

  if (error !== "") {
    return <p className="text-[#d98278]">{error}</p>;
  }
  if (usage === null) {
    return <p className="text-[#8f8b82]">Loading…</p>;
  }

  return (
    <div className="flex flex-col gap-6">
      <h2 className="text-lg text-[#f4f0e8]">Usage</h2>
      {rowsFor(usage).map((section) => (
        <div key={section.group} className="flex flex-col gap-1.5">
          <div className="text-sm font-medium text-[#8f8b82]">{section.group}</div>
          {section.rows.map((row) => (
            <div key={row.label} className="flex justify-between border-b border-[#343432] py-1.5 text-sm">
              <span className="text-[#cfccc3]">{row.label}</span>
              <span className="tabular-nums text-[#f4f0e8]">{row.value}</span>
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ui && npx vitest run src/settings/UsagePanel.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/src/settings/UsagePanel.tsx ui/src/settings/UsagePanel.test.tsx
git commit -m "feat(usage): add UsagePanel"
```

### Task 11: SettingsModal

**Files:**
- Create: `ui/src/settings/SettingsModal.tsx`

- [ ] **Step 1: Write the component** (layout from the screenshot: left nav with "Settings" header + a single "Usage" entry; right pane; close X top-right; no search box)

```tsx
import { useEffect } from "react";
import { Icon } from "../chat/Icon";
import { UsagePanel } from "./UsagePanel";

/**
 * SettingsModal — centered overlay modal. The left nav currently has a single
 * entry (Usage); the structure leaves room for more later. There is deliberately
 * no search box (per design).
 */
export function SettingsModal({ onClose }: { onClose(): void }) {
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Settings"
      onClick={onClose}
    >
      <div
        className="flex h-[560px] w-full max-w-[960px] overflow-hidden rounded-2xl border border-[#343432] bg-[#262624] shadow-[0_24px_60px_rgba(0,0,0,0.5)]"
        onClick={(event) => event.stopPropagation()}
      >
        <nav className="w-[220px] shrink-0 border-r border-[#343432] bg-[#21211f] p-3">
          <div className="px-2 pb-2 pt-1 text-xs font-medium uppercase tracking-wide text-[#807d74]">Settings</div>
          <button
            className="flex w-full items-center gap-2.5 rounded-md bg-[#343433] px-2.5 py-2 text-left text-sm text-[#f4f0e8]"
            type="button"
            aria-current="page"
          >
            <Icon name="sliders" size="18px" className="shrink-0" />
            Usage
          </button>
        </nav>
        <div className="relative flex-1 overflow-y-auto p-6">
          <button
            className="absolute right-4 top-4 grid h-8 w-8 place-items-center rounded-md text-[#aaa79e] hover:bg-[#2a2a28]"
            type="button"
            aria-label="Close settings"
            onClick={onClose}
          >
            <Icon name="close" size="18px" />
          </button>
          <UsagePanel />
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd ui && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add ui/src/settings/SettingsModal.tsx
git commit -m "feat(usage): add SettingsModal with Usage nav"
```

---

## Phase 8 — Wire the menu + modal into ChatShell

### Task 12: Replace inline Logout with the user-row menu and modal

**Files:**
- Modify: `ui/src/chat/ChatShell.tsx`

Context: today (lines ~989–1006) the user row renders an avatar, name, role label, and an inline `Logout` button calling `onLogout`. Replace the row so clicking name/role/empty space opens `UserMenu`; `Settings` opens `SettingsModal`.

- [ ] **Step 1: Add imports** (with the other imports at the top)

```tsx
import { UserMenu } from "./UserMenu";
import { SettingsModal } from "../settings/SettingsModal";
```

- [ ] **Step 2: Add state** (inside the component, near other `useState` hooks such as `openThreadMenuID`)

```tsx
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
```

- [ ] **Step 3: Replace the user-row block** (the `<div className="border-t border-[#343432] px-3 py-3">…</div>` at ~989–1006) with:

```tsx
        <div className="relative border-t border-[#343432] px-3 py-3">
          <div className={`flex items-center ${railCollapsed ? "justify-center" : "gap-3"}`}>
            <div className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-[#dedbd0] text-xs font-semibold text-[#1d1d1b]">
              {initialsFor(displayName)}
            </div>
            {!railCollapsed && (
              <button
                className="flex min-w-0 flex-1 items-center rounded-md px-1.5 py-1 text-left transition-colors hover:bg-[#2a2a28]"
                type="button"
                aria-haspopup="menu"
                aria-expanded={userMenuOpen}
                onClick={() => setUserMenuOpen((open) => !open)}
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[#f4f0e8]">{displayName}</div>
                  <div className="truncate font-normal text-[#8f8b82]">{roleLabel(user.role)}</div>
                </div>
              </button>
            )}
          </div>
          {userMenuOpen && !railCollapsed && (
            <>
              <div className="fixed inset-0 z-20" aria-hidden="true" onClick={() => setUserMenuOpen(false)} />
              <UserMenu
                className="bottom-full left-3 right-3 mb-2"
                onClose={() => setUserMenuOpen(false)}
                onSettings={() => setSettingsOpen(true)}
                onLogout={onLogout}
              />
            </>
          )}
        </div>
```

NOTE: the `UserMenu` `className` overrides its default to anchor it within the padded row (`left-3 right-3` ≈ full-width above the row). The invisible `fixed inset-0` layer closes the menu on outside click, mirroring how the app dismisses other popups.

- [ ] **Step 4: Render the modal** (near the end of the component, e.g. beside the existing mobile-sidebar overlay / other modals)

```tsx
      {settingsOpen && <SettingsModal onClose={() => setSettingsOpen(false)} />}
```

- [ ] **Step 5: Typecheck + run existing ChatShell tests**

Run: `cd ui && npx tsc --noEmit && npx vitest run src/chat`
Expected: no type errors; existing tests pass. If a test asserted the old inline `Logout` button text, update it to open the menu first (`click` the user row, then click `Log out`).

- [ ] **Step 6: Commit**

```bash
git add ui/src/chat/ChatShell.tsx
git commit -m "feat(usage): open user menu + settings modal from sidebar user row"
```

---

## Phase 9 — Autofocus search inputs

### Task 13: autoFocus on the three search fields

**Files:**
- Modify: `ui/src/chats/ChatsPage.tsx` (~line 226 input)
- Modify: `ui/src/projects/ProjectsPage.tsx` (~line 121 input)
- Modify: `ui/src/artifacts/ArtifactsPage.tsx` (~line 89 input)

- [ ] **Step 1: Add `autoFocus`** to each search `<input>`.

In `chats/ChatsPage.tsx`, the input with `placeholder="Search chats…"`:
```tsx
          <input
            type="text"
            autoFocus
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder="Search chats…"
            aria-label="Search chats"
            className="ui-composer-text h-11 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] pl-11 pr-3 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
          />
```

In `projects/ProjectsPage.tsx`, the input with `placeholder="Search projects..."`:
```tsx
          <input
            autoFocus
            className="ui-composer-text h-11 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] pl-11 pr-3 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
            placeholder="Search projects..."
            aria-label="Search projects"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
```

In `artifacts/ArtifactsPage.tsx`, the input with `placeholder="Search filenames..."`:
```tsx
          <input
            type="text"
            autoFocus
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder="Search filenames..."
            aria-label="Search filenames"
            className="ui-composer-text h-11 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] pl-11 pr-3 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
          />
```

- [ ] **Step 2: Typecheck + tests**

Run: `cd ui && npx tsc --noEmit && npx vitest run`
Expected: no errors; full frontend suite passes.

- [ ] **Step 3: Commit**

```bash
git add ui/src/chats/ChatsPage.tsx ui/src/projects/ProjectsPage.tsx ui/src/artifacts/ArtifactsPage.tsx
git commit -m "feat(ui): autofocus search inputs on chats, projects, artifacts pages"
```

---

## Final verification

- [ ] **Backend:** `cd backend && go build ./... && go test ./...` — all pass.
- [ ] **Frontend:** `cd ui && npx tsc --noEmit && npx vitest run` — all pass.
- [ ] **Manual smoke (see memory: slopr local verify):** run backend + UI, send a chat that triggers a web search / fetch / image gen, then open the sidebar user row → Settings → Usage and confirm counters increased; delete that chat and confirm the counters do **not** drop.

---

## Self-Review notes (addressed)

- **Spec coverage:** tokens (Task 6), web search/fetch (Task 5), obscura via fallback only (Task 5, Step 3 — failed fetch not counted), image gen (Task 6), chats/projects (Task 6), survive-deletion (Task 1 table is independent of messages/threads), endpoint + memory length (Task 7), user menu (Task 9/12), settings modal + usage panel no search (Task 10/11), Language dead entry (Task 9), autofocus (Task 13).
- **Token timing:** read of `usageTotal.Total()` is placed after the existing `titles.wait()` (Task 6, Step 2), so helper-call tokens are included — consistent with per-message stats. Async memory-refresh tokens are intentionally out of scope (they use a detached context without the accumulator).
- **Type consistency:** `usage.TokenDelta`, `usage.Totals`, `UsageStore` methods, frontend `Usage` field names, and JSON tags all match across tasks.
- **Open during execution:** confirm the exact existing test helpers for the httpapi handler test (Task 7 note); optionally swap the inline logout SVG for an icon-font glyph via the specimen.
