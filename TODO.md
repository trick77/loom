# TODO

Open items from the simplification and performance pass (PR #630). Each is its own change.

## Follow-ups to #630

- **Round budget for the obscura fallback.** The fallback runs one call at a time with up to 30s
  each (`backend/internal/httpapi/tool_dispatch.go`, `finishToolCall`). A round of a dozen hanging
  fetches can add minutes of silence on the stream. Cap the fallback time per round.
- **Per-host limit for concurrent fetches.** `startToolRuns` runs four fetches at once with no
  per-host bound; a strict site may rate-limit a paste of many links to one host, and each failure
  then takes the slower fallback.
- **Index build at boot.** Migration 0034 builds `idx_messages_user_thread` under the write lock on
  first boot. Fine today; revisit if a migration ever has to index a large table again.

## Frontend

- **Move stream runs and drafts out of `ThreadShell` state** into an external store
  (`useSyncExternalStore`), so the shell subscribes only to coarse selectors and a token or a
  keystroke re-renders the panel that shows it. `sendContent` reads `runs`; medium risk.
- **Split `ThreadShell`** (about 1600 lines): `useTurnSender` (send, incognito send, retries),
  `IncognitoShell`, `ShellModals`; move starring into `useThreadActions` / `useProjectActions`.
- **Start-screen pending attachments** duplicate `useDocumentAttachments`; hold them in the hook's
  global scope.
- **`ThreadActionsContext`** instead of drilling the thread menu handlers through four layers. Do it
  with the store change: the context value must be stable.
- **Unify confirm/rename modals** (`projects/*Modal.tsx`, `chats/BulkDeleteModal.tsx`,
  `chat/threadModals.tsx`, `artifacts/ArtifactActionModals.tsx`) on one `ConfirmModal`. Visible: the
  two dialog shells differ in width, panel colour and click-outside behaviour.
- **Unify action menus and sidebar rows** (`ThreadActionsMenu`, `ProjectActionsMenu`,
  `ArtifactActionsMenu`, `ProjectSidebarMenu`, the two row components in `SidebarItems.tsx`).
  Visible: separator colour and width differ.
- **Hover state in CSS** on the thread, artifact and project lists (`hoveredID` state re-renders
  every row); the project page renders up to 1000 threads without virtualization.
- **Lazy-load the markdown stack** (react-markdown, highlight, KaTeX) behind Suspense. Visible: a
  brief unformatted flash.
- **One shared markdown renderer** for `ProseMarkdown`, `ReasoningContent` and `MemoryMarkdown`.
- **Split the trailing live text block at paragraph boundaries** so only the tail re-parses per
  chunk. Measure first.
- **`ThreadsPage` requests.** It fires an unused recents fetch (`useThreadSearch` with an empty
  query) and, while searching, a discarded `useInfiniteList` page. Dropping the recents fetch as is
  blanks the list for a moment when a search starts.
- **`useThreadSearch` swallows a 401** on `ThreadsPage`: `onSessionExpired` is not passed and is
  missing from the effect deps.
- **Mutation boilerplate**: the pending/try/catch/finally pattern repeats in `useProjectActions`,
  `useThreadActions`, `ThreadShell` and `ArtifactsPage`; a small `runAction` helper covers it.
- `previousUserMessage` and `threadCostThrough` are referenced only by tests.
- Eight `exhaustive-deps` lint warnings (`ThreadShell`, `useInfiniteList`, `useThreadSearch`,
  `SharedChatsPanel`, `ProjectsPage`).

## Backend

- **Embed `Deps` in `server`** (`httpapi/server.go`): about 28 fields are declared twice and copied
  one by one.
- **`authed` route adapter**: `currentUser` is repeated in 39 handlers, `requireThreadStore` in 29;
  the nil-store branches only run in tests.
- **One table for built-in tools** (`tool_dispatch.go`): schema, gate, handler and per-round cap are
  spread over four switches.
- **`AddMessage*` variants** (`chat/message_store.go`): four are test-only; one
  `AddMessage(ctx, MessageInsert)`. About 60 test call sites.
- **`INSERT ... RETURNING`** instead of insert-then-re-read (messages, threads, projects, shares,
  artifacts). Check the SQLite version inside the sqlite-vec build first.
- **`activity_trace` is stored twice** per assistant message (also inside `content_blocks`); the UI
  reads it only for messages without blocks. Storage contract.
- **FTS search filters by user after matching all users' rows** (`message_fts.user_id` is
  UNINDEXED). Needs an FTS rebuild; only matters with many users.
- **Image upload** buffers the file three times and counts thread uploads by listing them
  (`artifact_handlers.go`); stream it and count in SQL.
- **Attached documents are looked up twice per turn** (`sent_attachments.go`,
  `document_attachments.go`); resolve once and fetch full texts concurrently.
- **Limit parsing** exists three times with different bounds (`request_helpers.go`,
  `artifact_handlers.go`, `thread_handlers.go`).
- **Artifact handlers** repeat the owned-artifact lookup and the file-serving block four times.
- **`config.Load`** repeats the parse-and-validate block for each duration and integer.
- **`mcp`**: the initialize params and the `IsError` handling are duplicated between the remote and
  stdio clients.
- **Favicon candidates** are probed one after another.
- **Removable once safe**: the ignored `ReasoningEffort` request field (kept as a sink for stale
  browser tabs) and `Message.ToolCalls` (always `[]`; API change).
- **One-time boot fixes** (`ReconcileLegacyDocumentScopes`, `ScrubOutOfScopeMessageCitations`) still
  open a write transaction on every boot to read their marker.
