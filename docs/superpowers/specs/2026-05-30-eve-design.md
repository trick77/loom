# eve — Design Spec (v1)

> **Status:** Approved design, basis for the (per-phase) implementation plans.
> **Date:** 2026-05-30 · **Chosen UI direction:** A — Warm Editorial (Claude-inspired).

## Context

`eve` is a self-hosted, multi-user LLM chat app. **Goal:** give a small group of users a
**deliberately simple** interface to work with **work- and school-related documents, topics,
questions and information** — secure LLM access (deliberately not advertised, no age controls).
It takes cues from the lean UI of **AnythingLLM** and the interaction patterns of the
**Claude Web UI** (projects that bundle threads; project-less threads; starred/recent chats;
automatic thread naming after the first inference).

Greenfield: backend in **Go**, frontend in **React**, everything in containers (`Containerfile` +
`compose.yaml`). The chat model is currently **Xiaomi MiMo** (OpenAI-compatible, with tool-calling);
**OpenAI** provides the embeddings for RAG.

v1 outcome: a working, secure multi-user LLM app with chat, agents/tools (MCP), document RAG, a
personal file area (Artifacts) and a persistent memory.

**Guiding principle:** a simplistic interface. **Document-centric workflows** (upload → ask →
*cited* answer) come first. Rich capabilities (agents, tools, memory) are presented leanly via
**progressive disclosure**; feature bloat is avoided (YAGNI).

---

## Scope (v1 — everything included)

- Multi-user with a predefined `admin` account (configurable password), roles `admin` and `user`;
  the admin creates users (no open registration).
- Chat against MiMo with **SSE streaming**.
- **Projects** (bundle threads) + **project-less threads**; **starred** and **recent** chats;
  **auto-naming** of a thread after the first inference.
- **Archive & delete** for both threads and projects: archive (soft, reversible, hidden from active
  lists) and delete (hard, confirmed). Deleting a project cascades to all of its threads + messages
  and its RAG index.
- **Agents/tools via MCP** (config-driven, all provided at startup), incl. **SearXNG** (web search)
  and **Fetch**. Native agent loop via OpenAI tool-calling.
- **Document RAG**: upload → extraction (Tika) → chunking → OpenAI embeddings → `sqlite-vec`.
  Layered scope (user-global + per project).
- **Memory RAG**: a persistent, per-user memory store (auto-extracted + manually editable),
  injected semantically into the context.
- **Artifacts**: a browser over the personal Docker volume (view/add/remove files; a thread can save
  results there).
- Hardcoded base system prompt (for now) + dynamic context blocks (memory, knowledge) +
  **response-language directive**. UI in English; v1 **without** an i18n scaffold (deliberately
  minimal — later languages = UI refactoring). Response language: default "auto" (= language of the
  input), optional per-user override `response_language`.
- No TTS.

**Deliberately out of scope (phase 2+):** TTS, more languages + UI i18n scaffold, more roles,
per-project custom instructions, reranking, runtime management of MCP servers via the UI,
shared workspaces.

---

## Architecture overview

A **single Go binary** serves the JSON/SSE API **and** the embedded React build (`embed.FS`).
Persistence in **one SQLite file** (incl. `sqlite-vec` for vectors). One **manually provisioned
Docker volume** per user.

`compose.yaml` services:

- **eve** — Go binary (API + static assets), **lean image** (no node/python runtimes). Mounts
  per-user volumes under `/data/users/<user-id>/` and a data volume for the SQLite file. Talks to
  MCP servers over HTTP/SSE.
- **searxng** — its own service (web-search backend).
- **tika** — Apache Tika server (document extraction). OCR only with the **`-full` image variant**
  (bundles Tesseract) — pin and verify the image tag, otherwise drop the OCR claim.
- **MCP servers** — as **separate HTTP/SSE containers** (SearXNG MCP, Fetch, others). The MCP client
  also supports stdio, but the deployment uses dedicated containers (eve image stays lean).

### Tech decisions (settled)

| Area | Decision | Rationale |
|---|---|---|
| Frontend | React + Vite + Tailwind, Anthropic fonts (npm) | matches the AnythingLLM/Claude references |
| Delivery | Go embeds the Vite build via `embed.FS` | one binary, one container |
| DB | SQLite | no DB service, small scale |
| Vector store | `sqlite-vec` (same file) | no extra service |
| LLM client | one OpenAI-compatible Go client, 2 endpoints/models (MiMo chat + OpenAI embeddings) | big simplification |
| Streaming | SSE | matches OpenAI streaming, chat is one-directional |
| Auth | server-side sessions (httponly cookie, revocable) | easier to revoke than JWT |
| Tools | everything via MCP, config-driven (stdio **or** HTTP/SSE) | uniform tool layer, one client |
| Extraction | Apache Tika sidecar | robust AnythingLLM analog, broadest format coverage, no fragile Go doc libs |

### Build & deployment

- **Runtime-agnostic:** `Containerfile` + `compose.yaml` per the OCI standard — runs with **Podman and
  Docker**, no runtime-specific features.
- **Multi-stage Containerfile:** Node stage (`vite build` → `dist`) → Go stage (`dist` via `embed.FS`,
  build binary) → minimal final image.
- **`sqlite-vec` integration (verify early):** loadable C extension → either cgo via
  `mattn/go-sqlite3` (extension loading; then the base image is **not** distroless-static but
  debian-slim/alpine) **or** pure-Go via `ncruces/go-sqlite3` (WASM, no cgo). Lock the choice early.
- **MCP as separate containers:** the eve image stays lean (no foreign runtimes); MCP servers run as
  their own HTTP/SSE containers.
- **Secrets** via compose `environment`/`env_file`, never baked into the image. Per-user volumes are
  provisioned and mounted manually.

---

## Data model (SQLite)

- `users` (id, username uniq, password_hash [argon2id], role, response_language [auto|de|en|…], created_at)
- `sessions` (token, user_id, created_at, expires_at, last_seen_at)
- `projects` (id, user_id, name, description, archived_at NULL, created_at)
- `threads` (id, user_id, project_id NULL, title, starred, archived_at NULL, created_at, updated_at, last_message_at)
- `messages` (id, thread_id, role [user|assistant|tool], content, tool_calls JSON, citations JSON, created_at)
- `documents` (id, user_id, project_id NULL, volume_relpath, filename, mime, size, status, error, created_at, embedded_at)
- `chunks` (id, document_id, user_id, project_id NULL, ordinal, text, token_count)
  + sqlite-vec table `vec_chunks(embedding)` (keyed to `chunks.id`)
- `memories` (id, user_id, content, source [auto|manual], origin_thread_id NULL, importance,
  use_count, last_used_at, created_at, updated_at)
  + sqlite-vec table `vec_memories(embedding)`
- `settings` (key, value) — admin-editable settings; secrets/endpoints via ENV.

**Isolation is the product:** every query filters strictly by `user_id`. No cross-user access to
projects/threads/messages/documents/memories.

---

## Personal volume & layered RAG

- Per-user volume root: `/data/users/<user-id>/` — the **path is keyed off the immutable user id**,
  not off the (renamable, unicode-/case-sensitive, user-controlled) display name. Project folders are
  keyed off project id/slug, with collision handling.
- **Layered scope (folder = scope):** root files = user-global; a **project = subfolder**. An indexed
  file inherits its scope from the folder (`project_id` set = project-scoped, otherwise global).
- **Retrieval:** a thread in project P → chunks with `project_id = P` (optionally plus global ones);
  a project-less thread → global chunks (`project_id IS NULL`).
- **Same volume, index is authoritative:** uploads land in the volume; the Artifacts browser shows the
  whole volume tree, but **only explicitly indexed files are "knowledge"** (resolves the tension
  "temporary storage" vs. persistent index). "Add to knowledge" indexes a file; deleting/moving/
  renaming in the browser requires **explicit re-indexing**.
- **Reconciliation:** a light reconciliation pass (on loading the project/document view) marks
  documents whose file at `volume_relpath` is missing as `stale` and excludes them from retrieval. A
  **move across project-folder boundaries** only changes scope after re-indexing.

**Concrete structure (separated: `files/` + `projects/`):**

```
/data/users/<user-id>/
├─ files/                # RAG scope: GLOBAL (freely organizable, subfolders allowed)
│   └─ …
├─ projects/             # eve-managed, one folder per project (keyed to project id)
│   └─ <project-id>/     # RAG scope: this project
│       ├─ …
│       └─ outputs/      # thread results for this project
└─ .eve/                 # reserved, NOT shown in the Artifacts browser: upload staging (Tika hotdir), tmp
```

- The Artifacts browser shows only `files/` + `projects/<id>/`; `.eve/` stays hidden.
- Thread results are scope-consistent: project thread → `projects/<P>/outputs/`; project-less thread → `files/`.

**Volume sandbox (security-critical):** confine every file operation hard to the user's root —
`filepath.Clean`, prefix check, `EvalSymlinks`, hard rejection of `..`/absolute paths/symlink escape.
Defined behavior when a volume is not mounted (clear error, no crash).

---

## Key flows

### Chat/agent loop (SSE)

1. The user sends a message in a thread.
2. The backend builds the system message: hardcoded base prompt (+ **response-language directive**:
   default "auto = language of the input", otherwise `users.response_language`) + injected **memories**
   (semantic, top-k) + **RAG chunks** (layered, scoped); plus recent thread history.
3. Call MiMo (OpenAI-compatible) with `tools` = MCP-discovered tools; response streamed via SSE.
4. On `tool_calls`: the backend executes via the MCP client → result back into the conversation → a
   **new** streamed completion → loop until the final answer. Status events ("tool X running") are
   streamed to the UI (AnythingLLM style). **Tricky (phase 4):** the interleaving stream → detect
   `tool_calls` → pause → run MCP → resume a new stream; verify early that MiMo emits `tool_calls`
   **reliably in streaming mode**.
5. Final answer streamed; **citations** (RAG sources + web links) attached.
6. Follow-up (async): **auto-naming** of the thread (short LLM call) on the first exchange;
   **memory extraction** (extract durable facts → dedup → embed → store).

**Context budget:** system prompt + memories + RAG chunks + thread history must fit MiMo's context
window — fixed trimming priority (system > current question > memories > RAG > older history),
otherwise long threads break.

### Document ingestion

Upload into the volume → status `pending` → Tika HTTP call (text + metadata) → token-based chunking
(~500–800 tokens, ~10–15% overlap) → OpenAI embeddings → `sqlite-vec` → status `embedded`.
Errors are recorded in `documents.status/error` and surfaced in the UI.

### MCP integration

An **`mcp.json`** at startup describes each server as either stdio (`command`,`args`,`env`) or remote
(`url`, headers). On boot: connect, discover tools, offer them to the model as `tools`. The SearXNG
MCP talks to the `searxng` container; Fetch is another server. The set is fixed at runtime.
**Optional tool-approval card** (human-in-the-loop) for sensitive tools, enabled via configuration.

**Citations mechanism:** generic MCP tool results are plain `content` blocks with no citation schema →
they render in the thought card **without** a sources pill. Citations come from (1) **RAG retrieval**
and (2) a few **source-aware tool adapters** (SearXNG, Fetch) that extract URLs/sources from the tool
output in the backend and feed them as link citations into the unified "Sources" pill. "Like
AnythingLLM" refers exactly to these known tools, not to arbitrary MCP servers.

### Auth

Argon2id passwords; `admin` is created **only on first boot** from ENV; afterwards the password is
changed via the admin UI and later ENV changes are ignored (clearly documented so "I changed the ENV
and nothing happened" isn't surprising). Server-side sessions via httponly+secure+SameSite cookie;
CSRF protection for state-changing requests. Admin endpoints are role-gated.

### Threads & projects: archive / delete

- **Archive (soft, reversible):** a thread or project can be archived (`archived_at` set). Archived
  items are hidden from the active lists (Recents, Starred, Projects) and reachable via an "Archived"
  view; unarchiving restores them.
- **Delete (hard, confirmed, irreversible):**
  - Deleting a **thread** removes the thread + its messages.
  - Deleting a **project** cascades: it removes the project, **all of its threads + messages**, and the
    project's RAG index (`documents` / `chunks` / `vec_chunks`). Implemented via FK `ON DELETE CASCADE`
    (or an equivalent app-level transaction), always scoped by `user_id`.
- **Project files on disk:** deleting a project **also permanently removes** its volume folder
  `projects/<id>/` and all its contents (confirmed). The delete confirmation must clearly warn that the
  project's files on disk will be deleted, not just its threads.

### Memory: maintenance & size control

- **Read:** embed the query → `vec_memories` top-k (user_id, similarity threshold) → its own context
  block; the memory block is additionally **token-capped**.
- **Write (auto, async):** a salience prompt yields **only durable, generally useful** facts (nothing
  ephemeral). Per candidate: embed → similarity search → **ADD / UPDATE / REPLACE / SKIP** (small LLM
  merge decision) → refresh duplicates instead of piling up, replace contradictions.
- **Not too much:** a per-user **soft cap** (configurable). On overflow, eviction by a score from
  `last_used_at` + `use_count` + age; **manual memories are protected** (evicted after auto). Optional
  periodic **consolidation** (merge similar ones).
- **Not stale:** supersession on write; on conflict the `updated_at`-newer wins; rarely used auto
  memories are down-ranked in retrieval (optional TTL/review). **Manual** view/edit/delete in the
  memory panel = the ultimate correction + transparency.
- **v1 lean:** salience + threshold dedup + supersession + soft-cap eviction + token cap on read.
  **Later:** consolidation/summarization job, time-decay scoring, memory categories.

---

## UI / UX

Three-column layout (like AnythingLLM/Claude):

```
┌──────────────┬───────────────────────────────────┬─────────────────┐
│  eve         │  Thread title            [Artifacts]│  Context panel  │
│  + New chat  │ ───────────────────────────────────│  (switchable)   │
│  🔎 Search   │  ▸ User: ...                        │                 │
│              │  ▸ Assistant: ...                   │  • Sources      │
│  ★ Starred   │    ┌ Thought card (tool running…) ┐ │    (citations → │
│   · ...      │    └ ▾ collapsible              ┘  │     side panel) │
│              │    ┌ Tool approval (Approve/Reject)│  • Artifacts    │
│  Recents     │    [Sources ⬤⬤⬤ +2]               │    (volume      │
│   · ...      │                                     │     browser)    │
│              │ ───────────────────────────────────│  • Memory       │
│  Projects    │  [ @agent | 📎 | Message…    ▶ ]   │    (view/edit)  │
│   ▾ Project A│                                     │                 │
│      · Thread│                                     │                 │
│  ───────────  │                                     │                 │
│  👤 User menu │                                     │                 │
└──────────────┴───────────────────────────────────┴─────────────────┘
```

- **Left sidebar:** New chat, Search, Starred, Recents, Projects (expandable → threads),
  bottom user menu (Settings, Memory, Admin [admin only], Logout).
- **Center:** chat stream with type-discriminated messages; **one reusable, collapsible "thought"
  card** for tool progress *and* model reasoning — tool status ("running tool X…") collapses to a
  single line and expands to the full thought chain; if MiMo emits reasoning (`<think>`-style tokens)
  it is parsed into the same card. **Default-collapsed**, with expansion state preserved across the
  streaming→final transition. Inline **tool-approval card**; a **"Sources" pill** under answers; prompt
  input with `@agent`/tools menu and file attachment.
- **Right panel (contextual):** Sources side panel (citations incl. relevance score, web hits as
  equal link citations), **Artifacts** file browser, **Memory** management.
- **Admin UI:** user management (create/delete, password/role), settings (model endpoints, read-only
  view of the MCP config).

### Chosen visual direction: A — Warm Editorial (Claude-inspired)

Light, warm theme; calm, high-quality, "editorial". Lots of whitespace, soft rounded corners, subtle
borders. The tokens below are the **starting palette** — fine-tuned during the frontend phase with the
Visual Companion, where the **real Anthropic fonts** (npm) are also wired in (approximated by web
fonts in the mockups).

| Token | Value |
|---|---|
| Background (app) | `#faf7f2` |
| Panel (sidebar/rail) | `#f1ebe1` |
| Active item | `#e9dfce` |
| Border | `#e3dccf` |
| Text | `#2b2520` |
| Muted | `#9a8f7e` |
| Accent (clay/coral) | `#cc785c` (text on accent: `#ffffff`) |
| User bubble | `#efe7d8` |
| AI bubble | `#ffffff` (with border) |
| Thought chip | `#f4efe6` |
| Input | `#ffffff` |
| Radius | ~10–14px |
| Typography | Headings: Anthropic serif; body: Anthropic sans |

> Reference mockup (all three directions): `.superpowers/brainstorm/<session>/content/ui-directions.html`
> (not versioned). A dark theme is out of scope for v1 (optional later).

---

## Repo structure (monorepo)

```
eve/
  backend/                     # Go module
    cmd/eve/main.go
    internal/
      config/   auth/   http/        # router, middleware, SSE
      chat/     llm/                 # agent loop, OpenAI-compatible client
      mcp/      rag/                 # MCP client, ingest/embed/retrieve
      memory/   documents/           # memory store, Tika extraction
      store/    volume/              # SQLite+sqlite-vec, sandboxed FS
    web/embed.go                     # embeds frontend/dist
  frontend/                    # React + Vite + Tailwind
    src/...
  Containerfile                # multi-stage: Node build → Go build → minimal image
  compose.yaml                 # eve + searxng + tika (+ MCP servers)
  mcp.json                     # example MCP configuration
```

---

## Security (cross-cutting)

- Strict `user_id` filtering in **every** query.
- Volume sandbox against path traversal (see above) — the sharpest attack surface.
- Argon2id, secure session cookies, CSRF protection, role-gated admin endpoints.
- Optional tool approval for sensitive tools.

---

## Implementation order (incremental)

> Each phase gets its own detailed implementation plan (via `superpowers:writing-plans`).

**Step 0 (done):** UI design direction chosen — **A (Warm Editorial)**.

1. **Foundation:** repo scaffold, Go server + embedded React build, SQLite store, config,
   Containerfile/compose skeleton.
2. **Auth + multi-user:** sessions, admin bootstrap, user CRUD, role gating.
3. **Chat core:** projects/threads/messages, OpenAI-compatible client, SSE, chat with MiMo,
   auto-naming, starred/recents.
4. **MCP + agent loop:** MCP client + config loader, tool-calling loop, tool-use display,
   SearXNG + Fetch, optional tool approval.
5. **Documents + RAG:** volume sandbox, upload, Tika extraction, chunking, embeddings, `sqlite-vec`,
   layered retrieval, citations UI, Artifacts browser.
6. **Memory:** store, auto-extraction, manual UI, injection.
7. **Polish:** admin UI, settings, error handling, security hardening, final UI mockups.

---

## Verification (end-to-end)

- **Go tests:** `go test ./...` for store/auth/volume-sandbox/agent-loop/RAG-retrieval.
- **Frontend:** Vitest (Vite-based) for components/utils.
- **Manual via `compose up` (Podman/Docker):**
  1. Log in as `admin`, create a second user.
  2. Chat with MiMo (streaming visible), verify auto thread naming, star/recents.
  3. Upload a document → Tika extraction → ask a question → verify the **citation**.
  4. `@agent` web search → SearXNG hits + link citations; verify the Fetch tool.
  5. Memory: after a conversation verify an entry, edit it manually, see the effect in a new thread.
  6. Artifacts: browse the volume, add/remove a file, save a thread result.
  7. **Isolation:** a second user does **not** see the first user's threads/files/memories;
     path-traversal attempts (`..`, absolute paths, symlink) are rejected.
