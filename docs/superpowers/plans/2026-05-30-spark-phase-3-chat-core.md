# Spark Phase 3 Chat Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the core multi-user chat product: projects, threads, messages, starred/recents, OpenAI-compatible MiMo streaming, and first-exchange auto-naming.

**Architecture:** Store projects, threads, and messages in SQLite with strict `user_id` scoping at every query boundary. The HTTP API exposes JSON endpoints for project/thread CRUD plus a protected SSE send endpoint that persists the user message, streams assistant deltas from an injected chat client, persists the final assistant message, and names untitled threads after the first exchange. The React app becomes the real three-column chat surface while keeping MCP tools, RAG, file attachments, citations, and memory out of Phase 3.

**Tech Stack:** Go 1.25, stdlib `net/http`, SQLite via `ncruces/go-sqlite3`, Server-Sent Events, OpenAI-compatible `/v1/chat/completions` streaming, React 19, TypeScript, Tailwind v4, Vitest.

---

## Decisions Locked In This Phase

- Use `POST /api/threads/{id}/messages:stream` for sending a message and streaming an assistant response. It keeps the stream endpoint explicit and avoids overloading message collection semantics.
- Store assistant messages only after the stream completes successfully. If the client disconnects after the user message is stored, the user message remains and no partial assistant message is saved.
- Auto-name only threads whose title is still `New chat`, using a short non-streaming chat call after the first completed assistant response.
- Use the existing `internal/sse.Writer` for outbound browser SSE. The LLM client reads upstream OpenAI-compatible SSE chunks and translates them to app events.
- Store `tool_calls` and `citations` as JSON text columns with default empty arrays. MCP tools and RAG citation population are out of scope for Phase 3, but the message shape is ready for later phases.
- Keep CSRF out of Phase 3 unless a project-wide CSRF mechanism already exists when implementation starts. Auth cookie protection already exists; state-changing endpoints stay same-origin and authenticated.

## File Structure

- Create `backend/internal/store/migrations/0003_chat_core.sql`: project, thread, and message schema.
- Create `backend/internal/chat/model.go`: chat domain types shared by stores and HTTP handlers.
- Create `backend/internal/chat/store.go`: SQLite-backed project/thread/message store with user-scoped methods.
- Create `backend/internal/chat/store_test.go`: store behavior, recents/starred/archive/delete, and user isolation tests.
- Create `backend/internal/llm/client.go`: OpenAI-compatible chat client interface and HTTP implementation.
- Create `backend/internal/llm/client_test.go`: request shape, streaming chunk parsing, non-streaming title generation, error handling.
- Create `backend/internal/httpapi/chat_handlers.go`: JSON chat endpoints and SSE send endpoint.
- Modify `backend/internal/httpapi/server.go`: add chat and LLM dependencies, register protected chat routes.
- Modify `backend/internal/httpapi/server_test.go`: route-level auth, scoping, JSON, and stream tests.
- Modify `backend/cmd/spark/main.go`: initialize chat store and MiMo client from config.
- Modify `frontend/src/api.ts`: project/thread/message DTOs and API helpers.
- Create `frontend/src/api.test.ts`: API URL construction and SSE parsing tests.
- Modify `frontend/src/App.tsx`: three-column chat shell, project/thread lists, message stream, composer.
- Modify `frontend/src/App.test.tsx`: signed-in chat UI, new chat, send message, streaming update, starred/admin retention.
- Modify `README.md`: Phase 3 configuration and smoke test.

## API Contract

All endpoints require an authenticated session and scope data to the current user.

```text
GET    /api/projects
POST   /api/projects
PATCH  /api/projects/{projectID}
POST   /api/projects/{projectID}/archive
POST   /api/projects/{projectID}/unarchive
DELETE /api/projects/{projectID}

GET    /api/threads
POST   /api/threads
GET    /api/threads/{threadID}
PATCH  /api/threads/{threadID}
POST   /api/threads/{threadID}/star
POST   /api/threads/{threadID}/unstar
POST   /api/threads/{threadID}/archive
POST   /api/threads/{threadID}/unarchive
DELETE /api/threads/{threadID}

POST   /api/threads/{threadID}/messages:stream
```

`GET /api/threads` supports these query parameters:

```text
projectId=<id>     only threads in one project
projectId=null     only project-less threads
starred=true       only starred active threads
archived=true      archived threads instead of active threads
limit=<1..100>     default 30
```

Stream events emitted by `messages:stream`:

```text
event: user_message
data: {"id":"msg_user","threadId":"thr","role":"user","content":"Hello","createdAt":"2026-05-30T19:00:00Z"}

event: assistant_delta
data: {"content":"Hel"}

event: assistant_message
data: {"id":"msg_assistant","threadId":"thr","role":"assistant","content":"Hello","createdAt":"2026-05-30T19:00:01Z"}

event: thread
data: {"id":"thr","title":"Short generated title","updatedAt":"2026-05-30T19:00:02Z","lastMessageAt":"2026-05-30T19:00:01Z"}

event: done
data: {}
```

Error events use:

```text
event: error
data: {"error":"message"}
```

## Task 1: Chat Database Schema

**Files:**
- Create: `backend/internal/store/migrations/0003_chat_core.sql`
- Modify: `backend/internal/store/store_test.go`

- [ ] **Step 1: Write the failing migration test**

Extend `TestOpen_runsMigrations` in `backend/internal/store/store_test.go` so it expects migration count `3` and verifies the new tables:

```go
for _, table := range []string{"settings", "users", "sessions", "projects", "threads", "messages"} {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
		table,
	).Scan(&name)
	if err != nil {
		t.Fatalf("%s table missing: %v", table, err)
	}
}
```

Update every schema migration count assertion in that test from `2` to `3`.

- [ ] **Step 2: Verify the test fails**

Run:

```bash
make test
```

Expected: `backend/internal/store` fails because `projects`, `threads`, and `messages` do not exist and only two migrations are applied.

- [ ] **Step 3: Add the migration**

Create `backend/internal/store/migrations/0003_chat_core.sql`:

```sql
CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_projects_user_active ON projects(user_id, archived_at, updated_at DESC);

CREATE TABLE threads (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id TEXT REFERENCES projects(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    starred INTEGER NOT NULL DEFAULT 0 CHECK (starred IN (0, 1)),
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_message_at TEXT
);

CREATE INDEX idx_threads_user_recent ON threads(user_id, archived_at, last_message_at DESC, updated_at DESC);
CREATE INDEX idx_threads_user_starred ON threads(user_id, starred, archived_at, updated_at DESC);
CREATE INDEX idx_threads_project ON threads(project_id, archived_at, updated_at DESC);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
    thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content TEXT NOT NULL,
    tool_calls TEXT NOT NULL DEFAULT '[]',
    citations TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_messages_thread_created ON messages(thread_id, created_at, id);
CREATE INDEX idx_messages_user_created ON messages(user_id, created_at DESC);
```

- [ ] **Step 4: Verify and commit**

Run:

```bash
make test
```

Expected: PASS.

Commit:

```bash
git add backend/internal/store/migrations/0003_chat_core.sql backend/internal/store/store_test.go
git commit -m "feat: add chat database schema"
```

## Task 2: Chat Store

**Files:**
- Create: `backend/internal/chat/model.go`
- Create: `backend/internal/chat/store.go`
- Create: `backend/internal/chat/store_test.go`

- [ ] **Step 1: Write failing project and thread store tests**

Create `backend/internal/chat/store_test.go` with a local test DB helper that imports the real `store.Open`, inserts test users, and covers project/thread creation:

```go
func TestStore_CreateProjectAndThreadScopesByUser(t *testing.T) {
	db := openTestDB(t)
	alice := insertTestUser(t, db, "alice")
	bob := insertTestUser(t, db, "bob")
	store := NewStore(db)

	project, err := store.CreateProject(context.Background(), alice.ID, CreateProjectInput{
		Name: "School",
		Description: "Homework context",
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	thread, err := store.CreateThread(context.Background(), alice.ID, CreateThreadInput{
		ProjectID: &project.ID,
		Title: "New chat",
	})
	if err != nil {
		t.Fatalf("CreateThread() error: %v", err)
	}

	if _, ok, err := store.GetThread(context.Background(), bob.ID, thread.ID); err != nil || ok {
		t.Fatalf("bob GetThread() = ok %v err %v, want ok false nil", ok, err)
	}
	got, ok, err := store.GetThread(context.Background(), alice.ID, thread.ID)
	if err != nil || !ok {
		t.Fatalf("alice GetThread() = ok %v err %v", ok, err)
	}
	if got.ProjectID == nil || *got.ProjectID != project.ID {
		t.Fatalf("project id = %v, want %q", got.ProjectID, project.ID)
	}
}
```

Add these concrete tests in the same file:

- `TestStore_ListThreadsSupportsRecentsAndStarred`: create two threads for one user, call `SetThreadStarred(ctx, user.ID, starredThread.ID, true)`, call `ListThreads(ctx, user.ID, chat.ListThreadsOptions{StarredOnly: true})`, and assert exactly one result with `ID == starredThread.ID`.
- `TestStore_ArchiveAndUnarchiveThread`: create one thread, archive it, assert the default `ListThreads` result is empty, assert `ListThreads(ctx, user.ID, chat.ListThreadsOptions{Archived: true})` returns that thread, unarchive it, and assert the default list returns it again.
- `TestStore_DeleteProjectCascadesThreadsAndMessages`: create a project, create a project thread, add user and assistant messages, delete the project, assert `GetThread(ctx, user.ID, thread.ID)` returns `ok=false`, and assert `ListMessages(ctx, user.ID, thread.ID)` returns `ok=false`.
- `TestStore_AddMessageUpdatesThreadLastMessageAt`: create one thread, read it, add a user message, read it again, and assert `LastMessageAt` is non-nil and not before the original `UpdatedAt`.

- [ ] **Step 2: Verify tests fail**

Run:

```bash
cd backend && go test ./internal/chat
```

Expected: package `internal/chat` or store symbols are missing.

- [ ] **Step 3: Add chat models**

Create `backend/internal/chat/model.go`:

```go
package chat

import "time"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

const DefaultThreadTitle = "New chat"

type Project struct {
	ID          string     `json:"id"`
	UserID      string     `json:"-"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	ArchivedAt  *time.Time `json:"archivedAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type Thread struct {
	ID            string     `json:"id"`
	UserID        string     `json:"-"`
	ProjectID     *string    `json:"projectId,omitempty"`
	Title         string     `json:"title"`
	Starred       bool       `json:"starred"`
	ArchivedAt    *time.Time `json:"archivedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	LastMessageAt *time.Time `json:"lastMessageAt,omitempty"`
}

type Message struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"threadId"`
	Role      Role      `json:"role"`
	Content   string    `json:"content"`
	ToolCalls string    `json:"toolCalls"`
	Citations string    `json:"citations"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateProjectInput struct {
	Name        string
	Description string
}

type UpdateProjectInput struct {
	Name        *string
	Description *string
}

type CreateThreadInput struct {
	ProjectID *string
	Title     string
}

type UpdateThreadInput struct {
	Title *string
}

type ListThreadsOptions struct {
	ProjectID        *string
	ProjectlessOnly  bool
	StarredOnly      bool
	Archived         bool
	Limit            int
}
```

- [ ] **Step 4: Implement the store**

Create `backend/internal/chat/store.go` with methods:

```go
type Store struct { db DBTX }

func NewStore(db DBTX) *Store
func (s *Store) CreateProject(ctx context.Context, userID string, in CreateProjectInput) (Project, error)
func (s *Store) ListProjects(ctx context.Context, userID string, archived bool) ([]Project, error)
func (s *Store) UpdateProject(ctx context.Context, userID, projectID string, in UpdateProjectInput) (Project, bool, error)
func (s *Store) SetProjectArchived(ctx context.Context, userID, projectID string, archived bool) (bool, error)
func (s *Store) DeleteProject(ctx context.Context, userID, projectID string) (bool, error)
func (s *Store) CreateThread(ctx context.Context, userID string, in CreateThreadInput) (Thread, error)
func (s *Store) GetThread(ctx context.Context, userID, threadID string) (Thread, bool, error)
func (s *Store) ListThreads(ctx context.Context, userID string, opts ListThreadsOptions) ([]Thread, error)
func (s *Store) UpdateThread(ctx context.Context, userID, threadID string, in UpdateThreadInput) (Thread, bool, error)
func (s *Store) SetThreadStarred(ctx context.Context, userID, threadID string, starred bool) (Thread, bool, error)
func (s *Store) SetThreadArchived(ctx context.Context, userID, threadID string, archived bool) (bool, error)
func (s *Store) DeleteThread(ctx context.Context, userID, threadID string) (bool, error)
func (s *Store) AddMessage(ctx context.Context, userID, threadID string, role Role, content string) (Message, error)
func (s *Store) ListMessages(ctx context.Context, userID, threadID string) ([]Message, bool, error)
```

Implementation requirements:

- Reuse the random URL-safe ID helper pattern from `backend/internal/auth/user_store.go`.
- Trim project names, project descriptions, thread titles, and message content with `strings.TrimSpace`.
- Reject empty project names and empty message content with clear errors.
- If `CreateThreadInput.Title` is empty after trimming, store `DefaultThreadTitle`.
- When creating a project thread, verify the project exists for the same `user_id` before inserting.
- `ListThreads` defaults `Limit` to `30`; clamp values above `100` to `100`.
- Every query includes `user_id = ?`; joins for project ownership also include `projects.user_id = ?`.
- Use `datetime('now')` updates in SQL for archive/star/title/message timestamp changes.

- [ ] **Step 5: Verify and commit**

Run:

```bash
cd backend && go test ./internal/chat
make test
```

Expected: PASS.

Commit:

```bash
git add backend/internal/chat/model.go backend/internal/chat/store.go backend/internal/chat/store_test.go
git commit -m "feat: add chat store"
```

## Task 3: OpenAI-Compatible Chat Client

**Files:**
- Create: `backend/internal/llm/client.go`
- Create: `backend/internal/llm/client_test.go`

- [ ] **Step 1: Write failing client tests**

Create tests with `httptest.Server`:

```go
func TestClient_StreamChatSendsOpenAICompatibleRequest(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"Hel"}}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer server.Close()

	client := NewClient(Config{BaseURL: server.URL + "/v1", APIKey: "secret", Model: "mimo-test"}, server.Client())
	var chunks []string
	final, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, func(delta string) error {
		chunks = append(chunks, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat() error: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if !gotBody.Stream || gotBody.Model != "mimo-test" {
		t.Fatalf("request = %+v", gotBody)
	}
	if strings.Join(chunks, "") != "Hello" || final != "Hello" {
		t.Fatalf("chunks %q final %q, want Hello", chunks, final)
	}
}
```

Add these concrete tests in the same file:

- `TestClient_GenerateTitleUsesNonStreamingRequest`: the test server decodes the request and asserts `stream` is absent or false, then returns `{"choices":[{"message":{"content":" \"Algebra help\" "}}]}` and the test asserts the returned title is `Algebra help`.
- `TestClient_StreamChatReturnsErrorForHTTP500`: the test server writes status `500` with body `{"error":"bad"}` and the test asserts `StreamChat` returns an error containing `500`.
- `TestClient_StreamChatPropagatesDeltaCallbackError`: the test server emits one chunk with content, the callback returns a sentinel error, and the test asserts `errors.Is(err, sentinel)` is true.

- [ ] **Step 2: Verify tests fail**

Run:

```bash
cd backend && go test ./internal/llm
```

Expected: package `internal/llm` or client symbols are missing.

- [ ] **Step 3: Implement the client**

Create `backend/internal/llm/client.go`:

```go
package llm

type Config struct {
	BaseURL string
	APIKey  string
	Model   string
}

type Message struct {
	Role    string
	Content string
}

type Client struct {
	cfg        Config
	httpClient *http.Client
}

func NewClient(cfg Config, httpClient *http.Client) *Client
func (c *Client) StreamChat(ctx context.Context, messages []Message, onDelta func(string) error) (string, error)
func (c *Client) GenerateTitle(ctx context.Context, userMessage, assistantMessage string) (string, error)
```

Implementation details:

- POST to `<BaseURL without trailing slash>/chat/completions`.
- Send JSON `{model, messages, stream}`. `stream` is `true` for `StreamChat` and `false` for `GenerateTitle`.
- Set `Authorization: Bearer <APIKey>` only when `APIKey` is non-empty.
- Parse upstream SSE lines beginning with `data: `. Ignore blank lines and comments.
- Stop on `data: [DONE]`.
- For each chunk, append `choices[0].delta.content` to a `strings.Builder` and call `onDelta(delta)` when `delta` is non-empty.
- Return an error for non-2xx responses that includes the status code and at most the first 4096 bytes of the body.
- `GenerateTitle` uses a system prompt: `Name this chat in 2 to 6 words. Return only the title.` It sends the user and assistant messages, reads `choices[0].message.content`, trims whitespace and surrounding quotes, limits the result to 80 runes, and returns `New chat` if the model returns an empty string.

- [ ] **Step 4: Verify and commit**

Run:

```bash
cd backend && go test ./internal/llm
make test
```

Expected: PASS.

Commit:

```bash
git add backend/internal/llm/client.go backend/internal/llm/client_test.go
git commit -m "feat: add openai compatible chat client"
```

## Task 4: HTTP Chat API

**Files:**
- Create: `backend/internal/httpapi/chat_handlers.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/cmd/spark/main.go`

- [ ] **Step 1: Write failing HTTP API tests**

Add fakes in `backend/internal/httpapi/server_test.go`:

```go
type fakeChatStore struct {
	projects []chat.Project
	threads  []chat.Thread
	messages []chat.Message
	thread   chat.Thread
}

func (f *fakeChatStore) CreateThread(ctx context.Context, userID string, in chat.CreateThreadInput) (chat.Thread, error) {
	f.thread = chat.Thread{ID: "thr_1", UserID: userID, Title: chat.DefaultThreadTitle}
	return f.thread, nil
}

type fakeChatClient struct {
	title string
}

func (f fakeChatClient) StreamChat(ctx context.Context, messages []llm.Message, onDelta func(string) error) (string, error) {
	if err := onDelta("Hel"); err != nil {
		return "", err
	}
	if err := onDelta("lo"); err != nil {
		return "", err
	}
	return "Hello", nil
}

func (f fakeChatClient) GenerateTitle(context.Context, string, string) (string, error) {
	return f.title, nil
}
```

Add route tests:

```go
func TestCreateThreadRequiresAuth(t *testing.T) {
	srv := New(Deps{Version: "test"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(`{}`))

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

```

Add these concrete tests in the same file:

- `TestCreateThreadReturnsThread`: use the existing authenticated fake session pattern, `POST /api/threads` with `{}`, assert status `201`, decode `chat.Thread`, and assert `Title == chat.DefaultThreadTitle`.
- `TestStreamMessageEmitsDeltasAndPersistsAssistant`: use an authenticated request to `POST /api/threads/thr_1/messages:stream` with `{"content":"Hi"}`, assert the body contains `event: assistant_delta`, `data: {"content":"Hel"}`, `event: assistant_message`, and `event: done`.
- `TestListThreadsUsesCurrentUserScope`: configure a fake store that records the `userID` passed to `ListThreads`, call `GET /api/threads`, and assert the captured user ID equals the authenticated fake user's ID.

- [ ] **Step 2: Verify tests fail**

Run:

```bash
make test
```

Expected: compile fails because chat dependencies and handlers are missing.

- [ ] **Step 3: Extend server dependencies**

In `backend/internal/httpapi/server.go`, add interfaces:

```go
type ChatStore interface {
	CreateProject(context.Context, string, chat.CreateProjectInput) (chat.Project, error)
	ListProjects(context.Context, string, bool) ([]chat.Project, error)
	UpdateProject(context.Context, string, string, chat.UpdateProjectInput) (chat.Project, bool, error)
	SetProjectArchived(context.Context, string, string, bool) (bool, error)
	DeleteProject(context.Context, string, string) (bool, error)
	CreateThread(context.Context, string, chat.CreateThreadInput) (chat.Thread, error)
	GetThread(context.Context, string, string) (chat.Thread, bool, error)
	ListThreads(context.Context, string, chat.ListThreadsOptions) ([]chat.Thread, error)
	UpdateThread(context.Context, string, string, chat.UpdateThreadInput) (chat.Thread, bool, error)
	SetThreadStarred(context.Context, string, string, bool) (chat.Thread, bool, error)
	SetThreadArchived(context.Context, string, string, bool) (bool, error)
	DeleteThread(context.Context, string, string) (bool, error)
	AddMessage(context.Context, string, string, chat.Role, string) (chat.Message, error)
	ListMessages(context.Context, string, string) ([]chat.Message, bool, error)
}

type ChatClient interface {
	StreamChat(context.Context, []llm.Message, func(string) error) (string, error)
	GenerateTitle(context.Context, string, string) (string, error)
}
```

Add `Chat ChatStore` and `LLM ChatClient` to `Deps` and `server`.

Register protected routes:

```go
mux.Handle("GET /api/projects", s.requireAuth(http.HandlerFunc(s.handleListProjects)))
mux.Handle("POST /api/projects", s.requireAuth(http.HandlerFunc(s.handleCreateProject)))
mux.Handle("PATCH /api/projects/{projectID}", s.requireAuth(http.HandlerFunc(s.handleUpdateProject)))
mux.Handle("POST /api/projects/{projectID}/archive", s.requireAuth(http.HandlerFunc(s.handleArchiveProject)))
mux.Handle("POST /api/projects/{projectID}/unarchive", s.requireAuth(http.HandlerFunc(s.handleUnarchiveProject)))
mux.Handle("DELETE /api/projects/{projectID}", s.requireAuth(http.HandlerFunc(s.handleDeleteProject)))
mux.Handle("GET /api/threads", s.requireAuth(http.HandlerFunc(s.handleListThreads)))
mux.Handle("POST /api/threads", s.requireAuth(http.HandlerFunc(s.handleCreateThread)))
mux.Handle("GET /api/threads/{threadID}", s.requireAuth(http.HandlerFunc(s.handleGetThread)))
mux.Handle("PATCH /api/threads/{threadID}", s.requireAuth(http.HandlerFunc(s.handleUpdateThread)))
mux.Handle("POST /api/threads/{threadID}/star", s.requireAuth(http.HandlerFunc(s.handleStarThread)))
mux.Handle("POST /api/threads/{threadID}/unstar", s.requireAuth(http.HandlerFunc(s.handleUnstarThread)))
mux.Handle("POST /api/threads/{threadID}/archive", s.requireAuth(http.HandlerFunc(s.handleArchiveThread)))
mux.Handle("POST /api/threads/{threadID}/unarchive", s.requireAuth(http.HandlerFunc(s.handleUnarchiveThread)))
mux.Handle("DELETE /api/threads/{threadID}", s.requireAuth(http.HandlerFunc(s.handleDeleteThread)))
mux.Handle("POST /api/threads/{threadID}/messages:stream", s.requireAuth(http.HandlerFunc(s.handleStreamMessage)))
```

- [ ] **Step 4: Implement chat handlers**

Create `backend/internal/httpapi/chat_handlers.go`.

Handler requirements:

- Read the current user with `auth.UserFromContext`.
- Return `503 {"error":"chat is not configured"}` when `s.chat == nil`.
- Decode JSON request bodies with `json.Decoder.DisallowUnknownFields()`.
- Return `404 {"error":"not found"}` when scoped store methods return `ok=false`.
- For `messages:stream`, return upstream deltas as app SSE events and persist the final assistant message after the LLM call returns.
- Build LLM history from persisted messages plus the new user message, using roles `system`, `user`, and `assistant`. The system prompt is:

```text
You are Spark, a concise assistant for work and school. Answer in the user's language unless their profile requests a specific response language.
```

- If `auth.User.ResponseLanguage` is not empty and not `auto`, append:

```text
Always answer in this language: <response_language>.
```

- After assistant persistence, if the thread title is `New chat`, call `GenerateTitle`, update the thread title, and emit a `thread` event.

- [ ] **Step 5: Wire the real dependencies**

In `backend/cmd/spark/main.go`, initialize:

```go
chatStore := chat.NewStore(db)
chatClient := llm.NewClient(llm.Config{
	BaseURL: cfg.ChatBaseURL,
	APIKey: cfg.ChatAPIKey,
	Model: cfg.ChatModel,
}, http.DefaultClient)
```

Pass `Chat: chatStore` and `LLM: chatClient` into `httpapi.New`.

- [ ] **Step 6: Verify and commit**

Run:

```bash
make test
```

Expected: PASS.

Commit:

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/chat_handlers.go backend/internal/httpapi/server_test.go backend/cmd/spark/main.go
git commit -m "feat: expose chat api"
```

## Task 5: Frontend API Client

**Files:**
- Modify: `frontend/src/api.ts`
- Create: `frontend/src/api.test.ts`

- [ ] **Step 1: Write failing API client tests**

Create `frontend/src/api.test.ts`:

```tsx
import { afterEach, expect, test, vi } from "vitest";
import { listThreads, streamMessage } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
});

test("listThreads builds query parameters", async () => {
  const fetchMock = vi.fn().mockResolvedValue(Response.json([]));
  vi.stubGlobal("fetch", fetchMock);

  await listThreads({ starred: true, limit: 10 });

  expect(fetchMock).toHaveBeenCalledWith("/api/threads?starred=true&limit=10");
});

test("streamMessage parses server-sent events", async () => {
  const body = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: assistant_delta\ndata: {"content":"Hel"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_delta\ndata: {"content":"lo"}\n\n'));
      controller.enqueue(encoder.encode('event: done\ndata: {}\n\n'));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(body, { status: 200 })));
  const deltas: string[] = [];

  await streamMessage("t1", "Hi", {
    onUserMessage: () => undefined,
    onDelta: (delta) => deltas.push(delta),
    onAssistantMessage: () => undefined,
    onThread: () => undefined,
  });

  expect(deltas.join("")).toBe("Hello");
});
```

- [ ] **Step 2: Verify tests fail**

Run:

```bash
make fe-test
```

Expected: `frontend/src/api.test.ts` fails because chat API helpers do not exist.

- [ ] **Step 3: Add typed API helpers**

In `frontend/src/api.ts`, add:

```ts
export type Project = {
  id: string;
  name: string;
  description: string;
  archivedAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type Thread = {
  id: string;
  projectId?: string;
  title: string;
  starred: boolean;
  archivedAt?: string;
  createdAt: string;
  updatedAt: string;
  lastMessageAt?: string;
};

export type Message = {
  id: string;
  threadId: string;
  role: "user" | "assistant" | "tool";
  content: string;
  createdAt: string;
};

export async function listProjects(): Promise<Project[]>;
export async function createProject(input: { name: string; description?: string }): Promise<Project>;
export async function listThreads(params?: { projectId?: string | null; starred?: boolean; archived?: boolean; limit?: number }): Promise<Thread[]>;
export async function createThread(input?: { projectId?: string | null; title?: string }): Promise<Thread>;
export async function getThread(threadId: string): Promise<{ thread: Thread; messages: Message[] }>;
export async function setThreadStarred(threadId: string, starred: boolean): Promise<Thread>;
export async function streamMessage(threadId: string, content: string, handlers: {
  onUserMessage(message: Message): void;
  onDelta(delta: string): void;
  onAssistantMessage(message: Message): void;
  onThread(thread: Thread): void;
}): Promise<void>;
```

`streamMessage` uses `fetch` with `POST`, reads `response.body.getReader()`, decodes UTF-8 chunks with `TextDecoder`, buffers until `\n\n`, parses `event:` and `data:` fields, and invokes handlers for the four app events.

- [ ] **Step 4: Verify and commit**

Run:

```bash
make fe-test
```

Expected: PASS.

Commit:

```bash
git add frontend/src/api.ts frontend/src/api.test.ts
git commit -m "feat: add chat frontend api client"
```

## Task 6: Frontend Chat Shell

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Write failing UI tests**

Add tests:

```tsx
test("creates a new chat from the sidebar", async () => {
  const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([]);
    if (url === "/api/threads" && init?.method === "POST") {
      return new Response(JSON.stringify({ id: "t1", title: "New chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" }), { status: 201 });
    }
    if (url === "/api/threads/t1") {
      return Response.json({ thread: { id: "t1", title: "New chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" }, messages: [] });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  await userEvent.click(await screen.findByRole("button", { name: /new chat/i }));

  expect(await screen.findByRole("heading", { name: "New chat" })).toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toBeInTheDocument();
});
```

Add a streaming test with a mocked `ReadableStream` response that emits `assistant_delta` and `assistant_message`, then assert the assistant text appears.

- [ ] **Step 2: Verify tests fail**

Run:

```bash
make fe-test
```

Expected: UI tests fail because the chat shell is still the Phase 2 placeholder.

- [ ] **Step 3: Implement the chat shell**

Modify `frontend/src/App.tsx` to keep these state slices:

```ts
const [projects, setProjects] = useState<Project[]>([]);
const [threads, setThreads] = useState<Thread[]>([]);
const [activeThread, setActiveThread] = useState<Thread | null>(null);
const [messages, setMessages] = useState<Message[]>([]);
const [draft, setDraft] = useState("");
const [streamingText, setStreamingText] = useState("");
const [isSending, setIsSending] = useState(false);
```

UI requirements:

- Preserve the signed-out screen and admin user list access from Phase 2.
- Left sidebar shows New chat, Starred section, Recents section, Projects section, and user menu.
- Center panel shows active thread title, message bubbles, streaming assistant text while a response is active, and a composer.
- Right panel shows a Sources tab placeholder with the text `Sources will appear with document and web answers.` and no fake citations.
- New chat creates a project-less thread, selects it, and loads its messages.
- Thread click loads `GET /api/threads/{id}`.
- Send validates non-empty draft, creates a thread first if none is selected, calls `streamMessage`, appends the user message event, appends deltas to `streamingText`, replaces the streaming text with the persisted assistant message, and updates the thread title when a `thread` event arrives.
- Disable send while streaming.
- Use existing theme classes: `bg-bg`, `bg-panel`, `bg-active`, `text-ink`, `text-muted`, `border-border`, `bg-accent`, `rounded-spark`, `font-serif`, `font-sans`.

- [ ] **Step 4: Verify and commit**

Run:

```bash
make fe-test
make fe-build
git checkout -- backend/web/dist/.gitkeep backend/web/dist/index.html
```

Expected: frontend tests and build pass; embedded dist placeholders are restored.

Commit:

```bash
git add frontend/src/App.tsx frontend/src/App.test.tsx frontend/src/api.ts
git commit -m "feat: add chat frontend shell"
```

## Task 7: README and Final Verification

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-05-30-spark-design.md` only if implementation intentionally diverged.

- [ ] **Step 1: Align README with Phase 3 behavior**

Ensure README includes:

- `SPARK_CHAT_BASE_URL`, `SPARK_CHAT_API_KEY`, and `SPARK_CHAT_MODEL`.
- Authenticated chat smoke test steps: sign in, create a new chat, send a message, see streamed response, confirm the thread title changes.
- A note that MCP tools, RAG/document upload, citations, artifacts, and memory are planned phases and are not enabled by Phase 3.

- [ ] **Step 2: Run full verification**

Run:

```bash
make test
make fe-test
make fe-build
make build
git checkout -- backend/web/dist/.gitkeep backend/web/dist/index.html
git status --short
```

Expected:

- Backend tests pass.
- Frontend tests pass.
- Frontend build passes.
- Full binary build passes.
- `backend/web/dist/.gitkeep` and `backend/web/dist/index.html` are not modified.
- Only intended source/docs files are changed.

- [ ] **Step 3: Commit docs alignment if needed**

If README or design docs changed:

```bash
git add README.md docs/superpowers/specs/2026-05-30-spark-design.md
git commit -m "docs: document chat core setup"
```

- [ ] **Step 4: Handoff**

Summarize:

- Commits created.
- Verification commands and pass/fail output.
- Manual MiMo configuration still required for a real streaming smoke test.
- Any known gaps intentionally left for Phase 4+.

## Self-Review

- Spec coverage: projects, project-less threads, messages, SSE chat with MiMo, response-language directive, auto-naming, starred threads, recents, archive/delete, and frontend three-column chat surface are covered.
- Out of scope check: MCP tool calling, tool-use display, RAG, citations population, file attachments, artifacts browser, and memory are explicitly excluded from Phase 3 and remain in later phases.
- Placeholder scan: this plan contains concrete file paths, method names, routes, request/response shapes, commands, expected outcomes, and commit messages.
- Type consistency: `chat.Project`, `chat.Thread`, `chat.Message`, `llm.Message`, `ChatStore`, and `ChatClient` names are introduced before later tasks use them.
- Security check: every store and route task requires authenticated user scoping; cross-user access is tested in the store and route layers.
