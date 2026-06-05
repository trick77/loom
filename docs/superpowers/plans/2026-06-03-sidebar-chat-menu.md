# Sidebar Chat Menu Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the approved sidebar chat action menu, rename/delete modals, and header MCP status relocation.

**Architecture:** Keep the change inside the existing React/Vite frontend and existing Go API surface. Add focused API helpers in `frontend/src/api.ts`, then extend `ChatShell.tsx` with small local components for sidebar rows, menus, modals, and icons. Use existing backend endpoints; no migration or backend route changes are expected.

**Tech Stack:** React 19, TypeScript, Vite, Vitest, Testing Library, existing Go `net/http` API.

---

## File Structure

- Modify `frontend/src/api.ts`: add `updateThread()` and `deleteThread()` helpers.
- Modify `frontend/src/api.test.ts`: cover the new helpers.
- Modify `frontend/src/ChatShell.tsx`: add menu/modal state, handlers, sidebar row components, modal components, icons, and header MCP status placement.
- Modify `frontend/src/App.test.tsx`: cover action menu, rename, inert Add to project, delete, and MCP status relocation.
- No backend files should change unless tests reveal the existing `PATCH /api/threads/{threadID}` or `DELETE /api/threads/{threadID}` handlers are insufficient.

## Task 1: Frontend Thread API Helpers

**Files:**
- Modify: `frontend/src/api.ts`
- Modify: `frontend/src/api.test.ts`

- [ ] **Step 1: Write failing API helper tests**

Append these tests to `frontend/src/api.test.ts` and update the import list to include `deleteThread` and `updateThread`.

```ts
test("updateThread patches the thread title", async () => {
  const updated = {
    id: "t1",
    title: "Renamed chat",
    starred: false,
    createdAt: "2026-05-30T00:00:00Z",
    updatedAt: "2026-05-30T00:00:01Z",
  };
  const fetchMock = vi.fn().mockResolvedValue(Response.json(updated));
  vi.stubGlobal("fetch", fetchMock);

  await expect(updateThread("t1", { title: "Renamed chat" })).resolves.toEqual(updated);
  expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1", {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title: "Renamed chat" }),
  });
});

test("deleteThread deletes a thread", async () => {
  const fetchMock = vi.fn().mockResolvedValue(new Response("", { status: 204 }));
  vi.stubGlobal("fetch", fetchMock);

  await expect(deleteThread("t1")).resolves.toBeUndefined();
  expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1", { method: "DELETE" });
});
```

- [ ] **Step 2: Run tests and verify failure**

Run: `npm run test -- --run src/api.test.ts`

Expected: fail with missing `updateThread` and `deleteThread` exports.

- [ ] **Step 3: Implement the helpers**

In `frontend/src/api.ts`, add:

```ts
export async function updateThread(
  threadId: string,
  input: { title?: string },
): Promise<Thread> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return expectJSON<Thread>(response, "failed to update thread");
}

export async function deleteThread(threadId: string): Promise<void> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}`, {
    method: "DELETE",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to delete thread");
  }
}
```

- [ ] **Step 4: Run tests and verify pass**

Run: `npm run test -- --run src/api.test.ts`

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api.ts frontend/src/api.test.ts
git commit -m "feat(ui): add thread mutation API helpers"
```

## Task 2: Sidebar Action Menu Rendering

**Files:**
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Write failing menu rendering tests**

Add tests to `frontend/src/App.test.tsx`:

```tsx
test("active sidebar chat shows actions menu with locked entries", async () => {
  vi.stubGlobal("fetch", chatThreadFetch(null, [
    { id: "m1", role: "assistant", content: "Earlier answer" },
  ]));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));

  expect(await screen.findByRole("menu", { name: "Chat actions" })).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: /^Star$/ })).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: "Rename" })).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: "Add to project" })).toBeDisabled();
  expect(screen.getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
});

test("add to project is inert", async () => {
  const fetchMock = chatThreadFetch(null);
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Add to project" }));

  expect(fetchMock.mock.calls.filter(([url]) => String(url).includes("project"))).toHaveLength(1);
});
```

The second test expects only the initial `/api/projects` call.

- [ ] **Step 2: Run tests and verify failure**

Run: `npm run test -- --run src/App.test.tsx -t "active sidebar chat shows actions menu|add to project is inert"`

Expected: fail because there is no `Open chat actions` button or menu.

- [ ] **Step 3: Add sidebar menu state and handlers**

In `ChatShell`, add state near the other sidebar state:

```tsx
const [openThreadMenuID, setOpenThreadMenuID] = useState<string | null>(null);
```

Add a close effect:

```tsx
useEffect(() => {
  if (openThreadMenuID === null) return;
  function handleKeyDown(event: KeyboardEvent) {
    if (event.key === "Escape") setOpenThreadMenuID(null);
  }
  window.addEventListener("keydown", handleKeyDown);
  return () => window.removeEventListener("keydown", handleKeyDown);
}, [openThreadMenuID]);
```

- [ ] **Step 4: Replace `SidebarSection` button rows with action-aware rows**

Change `SidebarSection` props to accept menu state:

```tsx
function SidebarSection({
  title,
  threads,
  activeThreadID,
  openThreadMenuID,
  onSelect,
  onToggleMenu,
  onCloseMenu,
}: {
  title: string;
  threads: Thread[];
  activeThreadID: string | null;
  openThreadMenuID: string | null;
  onSelect(threadID: string): void;
  onToggleMenu(threadID: string): void;
  onCloseMenu(): void;
}) {
  return (
    <section className="mt-5">
      <div className="slopr-meta-text mb-2 px-1.5 text-[#97958c]">{title}</div>
      <div className="space-y-1">
        {threads.map((thread) => (
          <SidebarThreadItem
            key={thread.id}
            thread={thread}
            active={activeThreadID === thread.id}
            menuOpen={openThreadMenuID === thread.id}
            onSelect={onSelect}
            onToggleMenu={onToggleMenu}
            onCloseMenu={onCloseMenu}
          />
        ))}
      </div>
    </section>
  );
}
```

Update both `<SidebarSection />` call sites with `openThreadMenuID`, `onToggleMenu={(threadID) => setOpenThreadMenuID((current) => current === threadID ? null : threadID)}`, and `onCloseMenu={() => setOpenThreadMenuID(null)}`.

- [ ] **Step 5: Add `SidebarThreadItem` and `ThreadActionsMenu`**

Place these below `SidebarSection`:

```tsx
function SidebarThreadItem({
  thread,
  active,
  menuOpen,
  onSelect,
  onToggleMenu,
  onCloseMenu,
}: {
  thread: Thread;
  active: boolean;
  menuOpen: boolean;
  onSelect(threadID: string): void;
  onToggleMenu(threadID: string): void;
  onCloseMenu(): void;
}) {
  if (!active) {
    return (
      <button
        className="block h-7 w-full truncate rounded-md px-1.5 text-left transition-colors hover:bg-[#2a2a28]"
        onClick={() => onSelect(thread.id)}
        type="button"
      >
        {thread.title}
      </button>
    );
  }
  return (
    <div className="relative">
      <div className="flex h-7 w-full items-center rounded-md bg-[#181817] py-0 pl-1.5 pr-1 text-left text-white">
        <button className="relative min-w-0 flex-1 overflow-hidden text-left" onClick={() => onSelect(thread.id)} type="button">
          <span className="block truncate pr-7">{thread.title}</span>
          <span className="pointer-events-none absolute inset-y-0 right-0 w-9 bg-gradient-to-r from-transparent to-[#181817]" aria-hidden="true" />
        </button>
        <button
          aria-expanded={menuOpen}
          aria-label="Open chat actions"
          className="grid h-6 w-6 shrink-0 place-items-center rounded-md text-[#d8d4ca] transition-colors hover:bg-[#2a2a28] hover:text-white"
          onClick={(event) => {
            event.stopPropagation();
            onToggleMenu(thread.id);
          }}
          type="button"
        >
          <span aria-hidden="true" className="text-lg leading-none">⋮</span>
        </button>
      </div>
      {menuOpen && <ThreadActionsMenu thread={thread} onClose={onCloseMenu} />}
    </div>
  );
}

function ThreadActionsMenu({ thread, onClose }: { thread: Thread; onClose(): void }) {
  return (
    <div
      aria-label="Chat actions"
      className="absolute left-[76px] z-20 mt-1 w-[188px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] shadow-[0_18px_32px_rgba(0,0,0,0.38)]"
      role="menu"
    >
      <button className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8]" role="menuitem" type="button" onClick={onClose}>
        <span className="w-[18px]" aria-hidden="true">{thread.starred ? "★" : "☆"}</span>
        {thread.starred ? "Unstar" : "Star"}
      </button>
      <button className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8]" role="menuitem" type="button" onClick={onClose}>
        <span className="w-[18px]" aria-hidden="true">✎</span>
        Rename
      </button>
      <button className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8] disabled:cursor-default disabled:opacity-100" disabled role="menuitem" type="button">
        <ProjectMenuIcon />
        Add to project
      </button>
      <div className="mx-[14px] my-[5px] h-px bg-[#77736b]" role="separator" />
      <button className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#d98278]" role="menuitem" type="button" onClick={onClose}>
        <TrashMenuIcon />
        Delete
      </button>
    </div>
  );
}
```

- [ ] **Step 6: Add menu icons**

Add:

```tsx
function ProjectMenuIcon() {
  return (
    <svg className="h-[18px] w-[18px] shrink-0" viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path d="M4.5 8.5h5l1.6 2h8.4v7.2c0 1.2-.7 1.8-2 1.8h-11c-1.3 0-2-.6-2-1.8V8.5Z" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
      <path d="M4.5 8.5V6.8c0-1.1.7-1.7 1.9-1.7h3.1l1.6 2h6.5c1.2 0 1.9.6 1.9 1.7v1.7" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
    </svg>
  );
}

function TrashMenuIcon() {
  return (
    <svg className="h-5 w-5 -ml-px shrink-0" viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path d="M8 7.5V6.2c0-.9.6-1.4 1.5-1.4h5c.9 0 1.5.5 1.5 1.4v1.3" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M5.5 7.5h13" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M7.2 9.5l.6 8.1c.1 1 .8 1.6 1.8 1.6h4.8c1 0 1.7-.6 1.8-1.6l.6-8.1" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
      <path d="M10.4 11.3v5M13.6 11.3v5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  );
}
```

- [ ] **Step 7: Run tests and verify pass**

Run: `npm run test -- --run src/App.test.tsx -t "active sidebar chat shows actions menu|add to project is inert"`

Expected: pass.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/ChatShell.tsx frontend/src/App.test.tsx
git commit -m "feat(ui): add sidebar chat action menu"
```

## Task 3: Star and Unstar From Sidebar Menu

**Files:**
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Write failing menu star test**

Add:

```tsx
test("stars and unstars a chat from the sidebar action menu", async () => {
  let starred = false;
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([{ ...threadFixture(), starred }]);
    if (url === "/api/threads/t1") return Response.json({ thread: { ...threadFixture(), starred }, messages: [] });
    if (url === "/api/threads/t1/star" && init?.method === "POST") {
      starred = true;
      return Response.json({ ...threadFixture(), starred: true });
    }
    if (url === "/api/threads/t1/unstar" && init?.method === "POST") {
      starred = false;
      return Response.json({ ...threadFixture(), starred: false });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Star" }));
  expect(await screen.findByRole("menuitem", { name: "Unstar" })).toBeInTheDocument();

  fireEvent.click(screen.getByRole("menuitem", { name: "Unstar" }));
  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1/unstar", { method: "POST" }));
});
```

- [ ] **Step 2: Run test and verify failure**

Run: `npm run test -- --run src/App.test.tsx -t "stars and unstars a chat from the sidebar action menu"`

Expected: fail because menu Star only closes menu.

- [ ] **Step 3: Wire menu action callback**

Pass a handler from `ChatShell` into `SidebarSection` and `ThreadActionsMenu`:

```tsx
async function handleSetThreadStarred(thread: Thread, starred: boolean) {
  if (isUpdatingStar) return;
  setIsUpdatingStar(true);
  try {
    const updatedThread = await setThreadStarred(thread.id, starred);
    if (activeThreadIDRef.current === updatedThread.id) setActiveThread(updatedThread);
    setThreads((current) => current.map((item) => (item.id === updatedThread.id ? updatedThread : item)));
    setOpenThreadMenuID(updatedThread.id);
    setSendError("");
  } catch (error) {
    handleActionError(error, "Thread failed to update.", setSendError);
  } finally {
    setIsUpdatingStar(false);
  }
}
```

Then call it from the menu Star/Unstar item:

```tsx
onStarChange(thread, !thread.starred);
```

- [ ] **Step 4: Remove or replace old `handleSetActiveThreadStarred` usage**

Remove `isUpdatingStar` and `onStarChange` props from `ChatPanel` after Task 6 moves the header control. For this task, keep `isUpdatingStar` state in `ChatShell` because the handler still needs to block duplicate requests.

- [ ] **Step 5: Run test and verify pass**

Run: `npm run test -- --run src/App.test.tsx -t "stars and unstars a chat from the sidebar action menu"`

Expected: pass.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/ChatShell.tsx frontend/src/App.test.tsx
git commit -m "feat(ui): move chat starring into sidebar menu"
```

## Task 4: Rename Modal Flow

**Files:**
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Write failing rename test**

Add:

```tsx
test("renames a chat from the sidebar menu", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([{ ...threadFixture(), title: "Existing chat" }]);
    if (url === "/api/threads/t1") return Response.json({ thread: { ...threadFixture(), title: "Existing chat" }, messages: [] });
    if (url === "/api/threads/t1" && init?.method === "PATCH") {
      return Response.json({ ...threadFixture(), title: "Renamed chat" });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Rename" }));

  const input = await screen.findByRole("textbox", { name: "Chat title" });
  expect(input).toHaveValue("Existing chat");
  fireEvent.change(input, { target: { value: "Renamed chat" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  expect(await screen.findByRole("button", { name: "Renamed chat" })).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads/t1",
    expect.objectContaining({
      method: "PATCH",
      body: JSON.stringify({ title: "Renamed chat" }),
    }),
  );
});
```

- [ ] **Step 2: Run test and verify failure**

Run: `npm run test -- --run src/App.test.tsx -t "renames a chat from the sidebar menu"`

Expected: fail because Rename does not open a modal.

- [ ] **Step 3: Import `updateThread`**

In `frontend/src/ChatShell.tsx`, add `updateThread` to the API import list.

- [ ] **Step 4: Add rename modal state and handler**

Add:

```tsx
const [renamingThread, setRenamingThread] = useState<Thread | null>(null);
const [renameTitle, setRenameTitle] = useState("");
const [modalError, setModalError] = useState("");
const [isMutatingThread, setIsMutatingThread] = useState(false);

function openRenameModal(thread: Thread) {
  setOpenThreadMenuID(null);
  setRenamingThread(thread);
  setRenameTitle(thread.title);
  setModalError("");
}

async function handleRenameSubmit() {
  if (renamingThread === null || isMutatingThread) return;
  const title = renameTitle.trim();
  if (title === "") return;
  setIsMutatingThread(true);
  try {
    const updatedThread = await updateThread(renamingThread.id, { title });
    if (activeThreadIDRef.current === updatedThread.id) setActiveThread(updatedThread);
    setThreads((current) => current.map((thread) => (thread.id === updatedThread.id ? updatedThread : thread)));
    setRenamingThread(null);
    setModalError("");
  } catch (error) {
    handleActionError(error, "Thread failed to rename.", setModalError);
  } finally {
    setIsMutatingThread(false);
  }
}
```

- [ ] **Step 5: Add `RenameThreadModal`**

Render it near the end of `ChatShell` root:

```tsx
{renamingThread !== null && (
  <RenameThreadModal
    title={renameTitle}
    error={modalError}
    disabled={isMutatingThread}
    onTitleChange={setRenameTitle}
    onCancel={() => setRenamingThread(null)}
    onSubmit={handleRenameSubmit}
  />
)}
```

Component:

```tsx
function RenameThreadModal({
  title,
  error,
  disabled,
  onTitleChange,
  onCancel,
  onSubmit,
}: {
  title: string;
  error: string;
  disabled: boolean;
  onTitleChange(value: string): void;
  onCancel(): void;
  onSubmit(): void;
}) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);
  return (
    <ModalShell title="Rename chat" onCancel={onCancel}>
      <form onSubmit={(event) => { event.preventDefault(); onSubmit(); }}>
        <input
          ref={inputRef}
          aria-label="Chat title"
          className="slopr-control-text mt-3 h-[38px] w-full rounded-lg border border-[#5b5851] bg-[#1f1f1d] px-3 text-[#f3f0e8] outline-none selection:bg-[#6f6250] selection:text-[#fffaf2]"
          value={title}
          onChange={(event) => onTitleChange(event.target.value)}
        />
        {error !== "" && <ErrorText>{error}</ErrorText>}
        <div className="mt-4 flex justify-end gap-2">
          <button className="h-8 rounded-md px-3 text-[#c7c5bd] hover:bg-[#363632]" onClick={onCancel} type="button">Cancel</button>
          <button className="h-8 rounded-md bg-[#50483d] px-3.5 font-medium text-[#fffaf2] disabled:opacity-50" disabled={disabled || title.trim() === ""} type="submit">Save</button>
        </div>
      </form>
    </ModalShell>
  );
}
```

- [ ] **Step 6: Add shared `ModalShell`**

```tsx
function ModalShell({ title, children, onCancel }: { title: string; children: React.ReactNode; onCancel(): void }) {
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onCancel();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onCancel]);
  return (
    <div className="fixed inset-0 z-40 grid place-items-center bg-[rgba(10,10,9,0.62)] px-4">
      <div className="w-full max-w-[390px] rounded-xl border border-[#4b4a46] bg-[#2a2a28] p-[18px] shadow-[0_28px_70px_rgba(0,0,0,0.55)]">
        <h2 className="font-sans text-[22px] font-semibold leading-7 text-[#f3f0e8]">{title}</h2>
        {children}
      </div>
    </div>
  );
}
```

- [ ] **Step 7: Wire Rename menu item**

Pass `onRename={openRenameModal}` down to `ThreadActionsMenu`; call `onRename(thread)` from the Rename item.

- [ ] **Step 8: Run test and verify pass**

Run: `npm run test -- --run src/App.test.tsx -t "renames a chat from the sidebar menu"`

Expected: pass.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/ChatShell.tsx frontend/src/App.test.tsx
git commit -m "feat(ui): add chat rename modal"
```

## Task 5: Delete Confirmation Flow

**Files:**
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Write failing delete test**

Add:

```tsx
test("deletes the active chat from the sidebar menu after confirmation", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([{ ...threadFixture(), title: "Existing chat" }]);
    if (url === "/api/threads/t1") return Response.json({ thread: { ...threadFixture(), title: "Existing chat" }, messages: [] });
    if (url === "/api/threads/t1" && init?.method === "DELETE") return new Response("", { status: 204 });
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));

  expect(await screen.findByRole("heading", { name: "Delete chat" })).toBeInTheDocument();
  expect(screen.getByText("Are you sure you want to delete this chat?")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));

  await waitFor(() => expect(window.location.pathname).toBe("/new"));
  expect(screen.queryByRole("button", { name: "Existing chat" })).not.toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1", { method: "DELETE" });
});
```

- [ ] **Step 2: Run test and verify failure**

Run: `npm run test -- --run src/App.test.tsx -t "deletes the active chat from the sidebar menu after confirmation"`

Expected: fail because Delete does not open a modal.

- [ ] **Step 3: Import `deleteThread`**

In `frontend/src/ChatShell.tsx`, add `deleteThread` to the API import list.

- [ ] **Step 4: Add delete modal state and handler**

Add:

```tsx
const [deletingThread, setDeletingThread] = useState<Thread | null>(null);

function openDeleteModal(thread: Thread) {
  setOpenThreadMenuID(null);
  setDeletingThread(thread);
  setModalError("");
}

async function handleDeleteConfirm() {
  if (deletingThread === null || isMutatingThread) return;
  const threadID = deletingThread.id;
  setIsMutatingThread(true);
  try {
    await deleteThread(threadID);
    setThreads((current) => current.filter((thread) => thread.id !== threadID));
    setDeletingThread(null);
    setModalError("");
    if (activeThreadIDRef.current === threadID) {
      streamAbortRef.current?.abort();
      activeThreadIDRef.current = null;
      setActiveThread(null);
      setMessages([]);
      setStreamingText("");
      setStreamingReasoning("");
      clearToolEvents();
      setSendError("");
      navigate({ view: "new" });
      setRoute({ view: "new" });
    }
  } catch (error) {
    handleActionError(error, "Thread failed to delete.", setModalError);
  } finally {
    setIsMutatingThread(false);
  }
}
```

- [ ] **Step 5: Add `DeleteThreadModal`**

Render:

```tsx
{deletingThread !== null && (
  <DeleteThreadModal
    error={modalError}
    disabled={isMutatingThread}
    onCancel={() => setDeletingThread(null)}
    onConfirm={handleDeleteConfirm}
  />
)}
```

Component:

```tsx
function DeleteThreadModal({
  error,
  disabled,
  onCancel,
  onConfirm,
}: {
  error: string;
  disabled: boolean;
  onCancel(): void;
  onConfirm(): void;
}) {
  return (
    <ModalShell title="Delete chat" onCancel={onCancel}>
      <p className="slopr-control-text mt-2 text-[#c7c5bd]">Are you sure you want to delete this chat?</p>
      {error !== "" && <ErrorText>{error}</ErrorText>}
      <div className="mt-[18px] flex justify-end gap-2">
        <button className="h-8 rounded-md px-3 text-[#c7c5bd] hover:bg-[#363632]" onClick={onCancel} type="button">Cancel</button>
        <button className="h-8 rounded-md bg-[#c9534b] px-3.5 font-semibold text-[#fff3ef] disabled:opacity-50" disabled={disabled} onClick={onConfirm} type="button">Delete</button>
      </div>
    </ModalShell>
  );
}
```

- [ ] **Step 6: Wire Delete menu item**

Pass `onDelete={openDeleteModal}` down to `ThreadActionsMenu`; call `onDelete(thread)` from the Delete item.

- [ ] **Step 7: Run test and verify pass**

Run: `npm run test -- --run src/App.test.tsx -t "deletes the active chat from the sidebar menu after confirmation"`

Expected: pass.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/ChatShell.tsx frontend/src/App.test.tsx
git commit -m "feat(ui): add chat delete confirmation"
```

## Task 6: Header MCP Status Relocation

**Files:**
- Modify: `frontend/src/ChatShell.tsx`
- Modify: `frontend/src/App.test.tsx`

- [ ] **Step 1: Write failing MCP relocation test**

Add:

```tsx
test("moves MCP status indicator into the chat header", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") return Response.json([{ ...threadFixture(), title: "Existing chat" }]);
      if (url === "/api/threads/t1") return Response.json({ thread: { ...threadFixture(), title: "Existing chat" }, messages: [] });
      if (url === "/api/mcp/status") return Response.json({ active: 1, configured: 2 });
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const header = await screen.findByRole("banner", { name: "Chat header" });
  expect(header).toContainElement(await screen.findByTitle("1 of 2 MCP servers active"));
  expect(screen.queryByRole("button", { name: "Star chat" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Unstar chat" })).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run test and verify failure**

Run: `npm run test -- --run src/App.test.tsx -t "moves MCP status indicator into the chat header"`

Expected: fail because header has no banner role/name and MCP indicator is still in the sidebar footer.

- [ ] **Step 3: Update `ChatPanel` props**

Replace `isUpdatingStar` and `onStarChange` props with:

```tsx
mcpStatus: McpStatusEvent | null;
```

Update the call site:

```tsx
<ChatPanel
  ...
  mcpStatus={mcpStatus}
  ...
/>
```

- [ ] **Step 4: Move MCP indicator into header and remove old star button**

Change the header to:

```tsx
<header
  aria-label="Chat header"
  className="slopr-control-text flex h-9 shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 text-[#d5d2c9]"
  role="banner"
>
  <h1 className="min-w-0 max-w-[28ch] truncate font-sans font-normal sm:max-w-[48ch]">
    {thread?.title ?? "New chat"}
    <span className="ml-2 text-[#88857d]" aria-hidden="true">⌄</span>
  </h1>
  <McpStatusIndicator status={mcpStatus} compact />
</header>
```

Remove the bottom sidebar footer rendering:

```tsx
{mcpStatus !== null && mcpStatus.configured > 0 && <McpStatusIndicator status={mcpStatus} />}
```

- [ ] **Step 5: Update `McpStatusIndicator` signature**

```tsx
function McpStatusIndicator({ status, compact = false }: { status: McpStatusEvent | null; compact?: boolean }) {
  if (status === null || status.configured <= 0) return null;
  const allActive = status.active === status.configured;
  const ringClass = allActive ? "border-success" : "border-danger";
  const dotClass = allActive ? "bg-success" : "bg-danger";
  return (
    <div
      className={`slopr-meta-text flex items-center gap-1.5 text-muted ${compact ? "" : "mt-2"}`}
      title={`${status.active} of ${status.configured} MCP servers active`}
    >
      <span className={`inline-flex h-3 w-3 items-center justify-center rounded-full border ${ringClass}`}>
        <span className={`h-1 w-1 rounded-full ${dotClass}`} />
      </span>
      <span>{status.active}</span>
    </div>
  );
}
```

- [ ] **Step 6: Run test and verify pass**

Run: `npm run test -- --run src/App.test.tsx -t "moves MCP status indicator into the chat header"`

Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/ChatShell.tsx frontend/src/App.test.tsx
git commit -m "feat(ui): move mcp status into chat header"
```

## Task 7: Full Frontend Verification and Visual QA

**Files:**
- Modify only if tests or visual QA reveal issues.

- [ ] **Step 1: Run targeted frontend tests**

Run: `npm run test -- --run src/api.test.ts src/App.test.tsx`

Expected: pass.

- [ ] **Step 2: Run frontend build**

Run: `npm run build`

Expected: TypeScript and Vite build pass.

- [ ] **Step 3: Restore generated backend web placeholders after build**

Run: `git checkout -- backend/web/dist/.gitkeep backend/web/dist/index.html`

Expected: build artifacts are not left as tracked changes.

- [ ] **Step 4: Start local dev server in current checkout**

Run: `npm run dev -- --host 127.0.0.1 --port 5174`

Expected: Vite serves the app at `http://127.0.0.1:5174/`.

- [ ] **Step 5: Browser QA**

Open `http://127.0.0.1:5174/` and verify:

- active sidebar row title fades instead of showing an ellipsis;
- vertical menu button opens the locked dropdown inside the sidebar divider;
- menu background fills the rounded top and bottom curves;
- divider is short, inset, and brighter than the menu background;
- Add to project uses the sidebar project icon and is inert;
- Delete uses the larger trashcan icon;
- Rename modal title is 22px semibold and input text is fully selected;
- Delete modal uses the locked red button background;
- MCP indicator appears in the chat header and no longer in the sidebar footer.

- [ ] **Step 6: Stop dev server**

Stop the running Vite process before completing the task.

- [ ] **Step 7: Final status**

Run: `git status --short`

Expected: no unwanted build artifacts. Any intentional code changes are committed.
