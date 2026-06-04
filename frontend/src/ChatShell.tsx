import {
  type ComponentPropsWithoutRef,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import type { ExtraProps } from "react-markdown";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import {
  AuthExpiredError,
  createProject,
  createThread,
  deleteThread,
  downloadArtifact,
  updateThread,
  getMcpStatus,
  getThread,
  listProjects,
  listThreads,
  setThreadStarred,
  streamMessage,
  type Artifact,
  type McpStatusEvent,
  type Message,
  type Project,
  type Thread,
  type ToolCallEvent,
  type ToolResultEvent,
  type User,
} from "./api";
import logoImage from "./assets/logo.png";
import { MessageMetrics } from "./MessageMetrics";

type ChatShellProps = {
  user: User;
  adminPanel: React.ReactNode;
  showAdmin: boolean;
  onAdmin(): void;
  onChat(): void;
  onLogout(): void;
  onSessionExpired(): void;
};

type RouteState =
  | { view: "new" }
  | { view: "chat"; threadID: string };

type ToolActivity = {
  id: string;
  name: string;
  status: "running" | "done";
  content?: string;
};

type MessageWithToolActivity = Message & {
  toolEvents?: ToolActivity[];
};

type SidebarIconName = "chats" | "projects";

export function ChatShell({
  user,
  adminPanel,
  showAdmin,
  onAdmin,
  onChat,
  onLogout,
  onSessionExpired,
}: ChatShellProps) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [threads, setThreads] = useState<Thread[]>([]);
  const [route, setRoute] = useState<RouteState>(() => routeFromLocation());
  const [activeThread, setActiveThread] = useState<Thread | null>(null);
  const [messages, setMessages] = useState<MessageWithToolActivity[]>([]);
  const [draft, setDraft] = useState("");
  const [projectName, setProjectName] = useState("");
  const [isProjectFormOpen, setIsProjectFormOpen] = useState(false);
  const [isCreatingProject, setIsCreatingProject] = useState(false);
  const [openThreadMenuID, setOpenThreadMenuID] = useState<string | null>(null);
  const [renamingThread, setRenamingThread] = useState<Thread | null>(null);
  const [deletingThread, setDeletingThread] = useState<Thread | null>(null);
  const [renameTitle, setRenameTitle] = useState("");
  const [modalError, setModalError] = useState("");
  const [isMutatingThread, setIsMutatingThread] = useState(false);
  const [streamingText, setStreamingText] = useState("");
  const [streamingReasoning, setStreamingReasoning] = useState("");
  const [streamingArtifacts, setStreamingArtifacts] = useState<Artifact[]>([]);
  const [toolEvents, setToolEvents] = useState<ToolActivity[]>([]);
  const [mcpStatus, setMcpStatus] = useState<McpStatusEvent | null>(null);
  const [sendError, setSendError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [isUpdatingStar, setIsUpdatingStar] = useState(false);
  const activeThreadIDRef = useRef<string | null>(null);
  const streamAbortRef = useRef<AbortController | null>(null);
  const toolEventsRef = useRef<ToolActivity[]>([]);

  const updateToolEvents = useCallback((updater: (current: ToolActivity[]) => ToolActivity[]) => {
    const next = updater(toolEventsRef.current);
    toolEventsRef.current = next;
    setToolEvents(next);
  }, []);

  const clearToolEvents = useCallback(() => {
    toolEventsRef.current = [];
    setToolEvents([]);
  }, []);

  function handleActionError(error: unknown, fallback: string, setError: (message: string) => void) {
    if (error instanceof AuthExpiredError) {
      onSessionExpired();
      return;
    }
    setError(error instanceof Error && error.message !== "" ? error.message : fallback);
  }

  useEffect(() => {
    if (window.location.pathname === "/") {
      window.history.replaceState({}, "", "/new");
      setRoute({ view: "new" });
    }
    function handlePopState() {
      setRoute(routeFromLocation());
    }
    window.addEventListener("popstate", handlePopState);
    return () => {
      window.removeEventListener("popstate", handlePopState);
    };
  }, []);

  useEffect(() => {
    if (openThreadMenuID === null) return;
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpenThreadMenuID(null);
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [openThreadMenuID]);

  useEffect(() => {
    let active = true;
    Promise.all([listProjects(), listThreads({ limit: 30 })])
      .then(([nextProjects, nextThreads]) => {
        if (!active) return;
        setProjects(nextProjects);
        setThreads(nextThreads);
        setLoadError("");
      })
      .catch((error: unknown) => {
        if (!active) return;
        if (error instanceof AuthExpiredError) {
          onSessionExpired();
          return;
        }
        setLoadError("Chat data failed to load.");
      });
    return () => {
      active = false;
      streamAbortRef.current?.abort();
    };
  }, [onSessionExpired]);

  useEffect(() => {
    let active = true;
    getMcpStatus()
      .then((status) => {
        if (!active) return;
        setMcpStatus(status);
      })
      .catch((error: unknown) => {
        if (!active) return;
        if (error instanceof AuthExpiredError) {
          onSessionExpired();
        }
      });
    return () => {
      active = false;
    };
  }, [onSessionExpired]);

  useEffect(() => {
    if (route.view !== "chat") {
      activeThreadIDRef.current = null;
      setActiveThread(null);
      setMessages([]);
      setStreamingText("");
      setStreamingArtifacts([]);
      clearToolEvents();
      setSendError("");
      return;
    }
    if (activeThreadIDRef.current === route.threadID) return;
    let active = true;
    streamAbortRef.current?.abort();
    // Drop any tool activity left over from the previous thread (e.g. a failed
    // turn whose panel is now kept) before this thread's transcript loads.
    clearToolEvents();
    getThread(route.threadID)
      .then((response) => {
        if (!active) return;
        setActiveThread(response.thread);
        activeThreadIDRef.current = response.thread.id;
        setMessages(response.messages);
        setStreamingText("");
        setStreamingArtifacts([]);
        clearToolEvents();
        setSendError("");
      })
      .catch((error: unknown) => {
        if (!active) return;
        handleActionError(error, "Chat failed to load.", setLoadError);
      });
    return () => {
      active = false;
    };
  }, [route]);

  const displayName = user.displayName || user.username;
  const starredThreads = useMemo(() => threads.filter((thread) => thread.starred), [threads]);

  const navigateToNew = useCallback(() => {
    onChat();
    streamAbortRef.current?.abort();
    activeThreadIDRef.current = null;
    setActiveThread(null);
    setMessages([]);
    setStreamingText("");
    setStreamingArtifacts([]);
    clearToolEvents();
    setSendError("");
    navigate({ view: "new" });
    setRoute({ view: "new" });
  }, [clearToolEvents, onChat]);

  async function selectThread(threadID: string) {
    onChat();
    navigate({ view: "chat", threadID });
    setRoute({ view: "chat", threadID });
  }

  async function handleCreateProject() {
    const name = projectName.trim();
    if (name === "" || isCreatingProject) return;
    setIsCreatingProject(true);
    try {
      const project = await createProject({ name });
      setProjects((current) => [project, ...current.filter((item) => item.id !== project.id)]);
      setProjectName("");
      setIsProjectFormOpen(false);
    } catch (error) {
      handleActionError(error, "Project failed to create.", setLoadError);
    } finally {
      setIsCreatingProject(false);
    }
  }

  async function handleSetThreadStarred(thread: Thread, starred: boolean, menuKey?: string) {
    if (isUpdatingStar) return;
    setIsUpdatingStar(true);
    try {
      const updatedThread = await setThreadStarred(thread.id, starred);
      if (activeThreadIDRef.current === updatedThread.id) {
        setActiveThread(updatedThread);
      }
      setThreads((current) =>
        current.map((item) => (item.id === updatedThread.id ? updatedThread : item)),
      );
      if (menuKey !== undefined) {
        setOpenThreadMenuID(null);
      }
      setSendError("");
    } catch (error) {
      handleActionError(error, "Thread failed to update.", setSendError);
    } finally {
      setIsUpdatingStar(false);
    }
  }

  function openRenameModal(thread: Thread) {
    setOpenThreadMenuID(null);
    setRenamingThread(thread);
    setRenameTitle(thread.title);
    setModalError("");
  }

  function openDeleteModal(thread: Thread) {
    setOpenThreadMenuID(null);
    setDeletingThread(thread);
    setModalError("");
  }

  async function handleRenameSubmit() {
    if (renamingThread === null || isMutatingThread) return;
    const title = renameTitle.trim();
    if (title === "") return;
    setIsMutatingThread(true);
    try {
      const updatedThread = await updateThread(renamingThread.id, { title });
      if (activeThreadIDRef.current === updatedThread.id) {
        setActiveThread(updatedThread);
      }
      setThreads((current) =>
        current.map((thread) => (thread.id === updatedThread.id ? updatedThread : thread)),
      );
      setRenamingThread(null);
      setModalError("");
    } catch (error) {
      handleActionError(error, "Thread failed to rename.", setModalError);
    } finally {
      setIsMutatingThread(false);
    }
  }

  async function handleDeleteConfirm() {
    if (deletingThread === null || isMutatingThread) return;
    setIsMutatingThread(true);
    try {
      await deleteThread(deletingThread.id);
      setThreads((current) => current.filter((thread) => thread.id !== deletingThread.id));
      if (activeThreadIDRef.current === deletingThread.id) {
        streamAbortRef.current?.abort();
        activeThreadIDRef.current = null;
        setActiveThread(null);
        setMessages([]);
        setStreamingText("");
        setStreamingReasoning("");
        setStreamingArtifacts([]);
        clearToolEvents();
        setSendError("");
        navigate({ view: "new" });
        setRoute({ view: "new" });
      }
      setDeletingThread(null);
      setModalError("");
    } catch (error) {
      handleActionError(error, "Thread failed to delete.", setModalError);
    } finally {
      setIsMutatingThread(false);
    }
  }

  async function handleSend() {
    const content = draft.trim();
    if (content === "" || isSending) return;
    await sendContent(content, { restoreDraftOnError: true });
  }

  async function handleRetry(content: string) {
    if (content.trim() === "" || isSending || activeThread === null) return;
    await sendContent(content, { restoreDraftOnError: false });
  }

  async function sendContent(content: string, options: { restoreDraftOnError: boolean }) {
    setDraft("");
    setIsSending(true);
    setStreamingText("");
    setStreamingReasoning("");
    setStreamingArtifacts([]);
    clearToolEvents();
    setSendError("");
    let abortController: AbortController | null = null;
    let createdThreadForFallback: Thread | null = null;
    let receivedThreadEvent = false;
    try {
      let targetThread = activeThread;
      if (targetThread === null) {
        targetThread = await createThread();
        createdThreadForFallback = targetThread;
        setActiveThread(targetThread);
        setMessages([]);
        navigate({ view: "chat", threadID: targetThread.id });
        setRoute({ view: "chat", threadID: targetThread.id });
      }
      const targetThreadID = targetThread.id;
      activeThreadIDRef.current = targetThreadID;
      abortController = new AbortController();
      streamAbortRef.current?.abort();
      streamAbortRef.current = abortController;
      const isCurrentThread = () => activeThreadIDRef.current === targetThreadID;
      await streamMessage(targetThreadID, content, {
        onUserMessage: (message) => {
          if (isCurrentThread()) setMessages((current) => [...current, message]);
        },
        onDelta: (delta) => {
          if (isCurrentThread()) setStreamingText((current) => current + delta);
        },
        onReasoningDelta: (delta) => {
          if (isCurrentThread()) setStreamingReasoning((current) => current + delta);
        },
        onToolCall: (event) => {
          if (!isCurrentThread()) return;
          updateToolEvents((current) => upsertToolCall(current, event));
        },
        onToolResult: (event) => {
          if (!isCurrentThread()) return;
          updateToolEvents((current) => upsertToolResult(current, event));
        },
        onArtifact: (artifact) => {
          if (!isCurrentThread()) return;
          setStreamingArtifacts((current) => [
            ...current.filter((item) => item.id !== artifact.id),
            artifact,
          ]);
        },
        onAssistantMessage: (message) => {
          if (!isCurrentThread()) return;
          const completedToolEvents = toolEventsRef.current;
          setMessages((current) => [
            ...current,
            completedToolEvents.length > 0 ? { ...message, toolEvents: completedToolEvents } : message,
          ]);
          setStreamingText("");
          setStreamingReasoning("");
          setStreamingArtifacts([]);
          clearToolEvents();
        },
        onThread: (updatedThread) => {
          receivedThreadEvent = true;
          if (isCurrentThread()) setActiveThread(updatedThread);
          setThreads((current) => upsertThread(current, updatedThread));
        },
        onMcpStatus: (event) => setMcpStatus(event),
      }, abortController.signal);
      const fallbackThread = createdThreadForFallback;
      if (!receivedThreadEvent && fallbackThread !== null) {
        setThreads((current) => upsertThread(current, fallbackThread));
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      setStreamingText("");
      setStreamingReasoning("");
      setStreamingArtifacts([]);
      // Keep any tool activity visible so a failed turn still shows what was
      // attempted (e.g. a tool that errored); the next send clears it.
      if (options.restoreDraftOnError) setDraft(content);
      handleActionError(error, "Message failed to send.", setSendError);
    } finally {
      setIsSending(false);
      if (abortController !== null && streamAbortRef.current === abortController) {
        streamAbortRef.current = null;
      }
    }
  }

  return (
    <div className="grid h-screen grid-cols-[362px_1fr] bg-bg font-sans text-ink">
      <aside className="spark-sidebar-text flex min-h-0 flex-col border-r border-[#343432] bg-panel pl-2 text-[#c7c5bd]">
        <div className="flex h-11 items-center justify-between px-3">
          <div className="spark-wordmark font-serif font-medium text-[#f4f0e8]">Spark</div>
          <div className="flex items-center gap-3 text-[#aaa79e]" aria-hidden="true">
            <span className="text-sm">⌕</span>
            <span className="text-xs">▯</span>
          </div>
        </div>
        <nav className="min-h-0 flex-1 overflow-y-auto px-2 pb-4 pt-2">
          <button
            className={`flex h-7 w-full items-center gap-2.5 rounded-md px-1.5 text-left transition-colors hover:bg-[#2a2a28] ${
              route.view === "new" && !showAdmin ? "bg-[#111110]" : ""
            }`}
            onClick={navigateToNew}
            type="button"
          >
            <span className="grid h-[18px] w-[18px] shrink-0 place-items-center rounded-full bg-[#30302e] text-[14px] leading-none text-[#d5d2c9]">
              +
            </span>
            <span>New chat</span>
          </button>
          <SidebarPrimaryItem label="Chats" icon="chats" />
          <SidebarPrimaryItem label="Projects" icon="projects" />
          {loadError !== "" && (
            <div className="spark-meta-text mx-1.5 mt-3 rounded-md border border-accent px-2 py-2 text-accent">
              {loadError}
            </div>
          )}
          <SidebarSection
            title="Starred"
            threads={starredThreads}
            activeThreadID={route.view === "chat" ? route.threadID : null}
            openThreadMenuID={openThreadMenuID}
            onSelect={selectThread}
            onDelete={openDeleteModal}
            onRename={openRenameModal}
            onStarChange={handleSetThreadStarred}
            onToggleMenu={(menuKey) =>
              setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey))
            }
            onCloseMenu={() => setOpenThreadMenuID(null)}
          />
          <SidebarSection
            title="Recents"
            threads={threads}
            activeThreadID={route.view === "chat" ? route.threadID : null}
            openThreadMenuID={openThreadMenuID}
            onSelect={selectThread}
            onDelete={openDeleteModal}
            onRename={openRenameModal}
            onStarChange={handleSetThreadStarred}
            onToggleMenu={(menuKey) =>
              setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey))
            }
            onCloseMenu={() => setOpenThreadMenuID(null)}
          />
          <section className="mt-5">
            <div className="spark-meta-text mb-2 flex items-center justify-between px-1.5 text-[#97958c]">
              <span>Projects</span>
              <button
                className="rounded px-1 text-[#aaa79e] transition-colors hover:text-white"
                onClick={() => setIsProjectFormOpen(true)}
                type="button"
                aria-label="New project"
              >
                +
              </button>
            </div>
            {isProjectFormOpen && (
              <form
                className="mx-2 mb-2 space-y-2"
                onSubmit={(event) => {
                  event.preventDefault();
                  handleCreateProject();
                }}
              >
                <input
                  autoFocus
                  className="spark-sidebar-text w-full rounded-md border border-[#3b3b38] bg-[#20201f] px-2 py-1.5 text-ink outline-none placeholder:text-muted focus:border-[#69665f]"
                  placeholder="Project name"
                  value={projectName}
                  onChange={(event) => setProjectName(event.target.value)}
                />
                <div className="flex gap-2">
                  <button
                    className="spark-sidebar-text rounded-md bg-[#393936] px-3 py-1.5 font-medium text-white disabled:opacity-50"
                    disabled={projectName.trim() === "" || isCreatingProject}
                    type="submit"
                  >
                    Create
                  </button>
                  <button
                    className="spark-sidebar-text px-2 py-1.5 text-muted transition-colors hover:text-ink"
                    onClick={() => {
                      setProjectName("");
                      setIsProjectFormOpen(false);
                    }}
                    type="button"
                  >
                    Cancel
                  </button>
                </div>
              </form>
            )}
            <div className="space-y-1.5">
              {projects.map((project) => (
                <div key={project.id} className="truncate rounded-md px-1.5 py-1.5 text-xs">
                  {project.name}
                </div>
              ))}
            </div>
          </section>
          {user.role === "admin" && (
            <button
              className="mt-3 flex h-7 w-full items-center rounded-md px-1.5 text-left transition-colors hover:bg-[#2a2a28]"
              onClick={onAdmin}
              type="button"
            >
              Admin
            </button>
          )}
        </nav>
        <div className="border-t border-[#343432] px-3 py-3">
          <div className="flex items-center gap-3">
            <div className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-[#dedbd0] text-xs font-semibold text-[#1d1d1b]">
              {initialsFor(displayName)}
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-[#f4f0e8]">{displayName}</div>
              <div className="truncate font-normal text-[#8f8b82]">{user.role}</div>
            </div>
            <button className="rounded-md px-2 py-1 text-[#aaa79e] hover:bg-[#2a2a28]" onClick={onLogout}>
              Logout
            </button>
          </div>
        </div>
      </aside>
      <main className="min-w-0 bg-bg">
        {showAdmin ? (
          adminPanel
        ) : route.view === "new" ? (
          <StartPanel
            displayName={displayName}
            draft={draft}
            isSending={isSending}
            sendError={sendError}
            onDraftChange={setDraft}
            onSend={handleSend}
          />
        ) : (
          <ChatPanel
            thread={activeThread}
            messages={messages}
            draft={draft}
            streamingText={streamingText}
            streamingReasoning={streamingReasoning}
            streamingArtifacts={streamingArtifacts}
            toolEvents={toolEvents}
            sendError={sendError}
            isSending={isSending}
            mcpStatus={mcpStatus}
            onDraftChange={setDraft}
            onSend={handleSend}
            onRetry={handleRetry}
          />
        )}
      </main>
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
      {deletingThread !== null && (
        <DeleteThreadModal
          error={modalError}
          disabled={isMutatingThread}
          onCancel={() => setDeletingThread(null)}
          onDelete={handleDeleteConfirm}
        />
      )}
    </div>
  );
}

function routeFromLocation(): RouteState {
  const path = window.location.pathname;
  if (path.startsWith("/chat/")) {
    const threadID = decodeURIComponent(path.slice("/chat/".length));
    if (threadID !== "") return { view: "chat", threadID };
  }
  return { view: "new" };
}

function navigate(route: RouteState) {
  const path = route.view === "new" ? "/new" : `/chat/${encodeURIComponent(route.threadID)}`;
  if (window.location.pathname !== path) {
    window.history.pushState({}, "", path);
  }
}

function upsertThread(current: Thread[], thread: Thread): Thread[] {
  return [thread, ...current.filter((item) => item.id !== thread.id)];
}

function initialsFor(name: string): string {
  const trimmed = name.trim();
  if (trimmed === "") return "S";
  return trimmed
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("");
}

function greetingForNow() {
  const hour = new Date().getHours();
  if (hour < 12) return "Morning";
  if (hour < 18) return "Afternoon";
  return "Evening";
}

function SidebarPrimaryItem({ icon, label }: { icon: SidebarIconName; label: string }) {
  return (
    <div className="flex h-7 items-center gap-2.5 rounded-md px-1.5 text-[#c7c5bd]">
      <SidebarIcon name={icon} />
      <span className="truncate">{label}</span>
    </div>
  );
}

function SidebarIcon({ name }: { name: SidebarIconName }) {
  const className = "h-5 w-5 shrink-0 text-[#f0eee7]";
  if (name === "chats") {
    return (
      <svg className={className} viewBox="0 0 24 24" aria-hidden="true" fill="none">
        <path
          d="M6.5 15.5c-2.2-.2-3.5-1.6-3.5-3.8V8.8C3 6.3 4.5 5 7.1 5h5.1c2.6 0 4.1 1.3 4.1 3.8v2.9c0 2.5-1.5 3.8-4.1 3.8H9l-3.3 2.3v-2.3Z"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinejoin="round"
        />
        <path
          d="M17.2 9.2c2.5.1 3.8 1.4 3.8 3.8v2.4c0 2.2-1.3 3.5-3.6 3.7v2l-2.9-2h-3.2c-1.8 0-3-.7-3.5-2"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    );
  }
  if (name === "projects") {
    return (
      <svg className={className} viewBox="0 0 24 24" aria-hidden="true" fill="none">
        <path
          d="M4.5 8.5h5l1.6 2h8.4v7.2c0 1.2-.7 1.8-2 1.8h-11c-1.3 0-2-.6-2-1.8V8.5Z"
          stroke="currentColor"
          strokeWidth="1.8"
          strokeLinejoin="round"
        />
        <path d="M4.5 8.5V6.8c0-1.1.7-1.7 1.9-1.7h3.1l1.6 2h6.5c1.2 0 1.9.6 1.9 1.7v1.7" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
      </svg>
    );
  }
  return null;
}

function McpStatusIndicator({ compact = false, status }: { compact?: boolean; status: McpStatusEvent }) {
  const allActive = status.active === status.configured;
  const ringClass = allActive ? "border-success" : "border-danger";
  const dotClass = allActive ? "bg-success" : "bg-danger";
  return (
    <div
      className={`spark-meta-text flex items-center gap-1.5 text-muted ${compact ? "" : "mt-2"}`}
      title={`${status.active} of ${status.configured} MCP servers active`}
    >
      <span className={`inline-flex h-3 w-3 items-center justify-center rounded-full border ${ringClass}`}>
        <span className={`h-1 w-1 rounded-full ${dotClass}`} />
      </span>
      <span>{status.active}</span>
    </div>
  );
}

function SidebarSection({
  title,
  threads,
  activeThreadID,
  openThreadMenuID,
  onSelect,
  onDelete,
  onRename,
  onStarChange,
  onToggleMenu,
  onCloseMenu,
}: {
  title: string;
  threads: Thread[];
  activeThreadID: string | null;
  openThreadMenuID: string | null;
  onSelect(threadID: string): void;
  onDelete(thread: Thread): void;
  onRename(thread: Thread): void;
  onStarChange(thread: Thread, starred: boolean, menuKey: string): void;
  onToggleMenu(menuKey: string): void;
  onCloseMenu(): void;
}) {
  return (
    <section className="mt-5">
      <div className="spark-meta-text mb-2 px-1.5 text-[#97958c]">{title}</div>
      <div className="space-y-1.5">
        {threads.map((thread) => (
          <SidebarThreadItem
            key={thread.id}
            menuKey={`${title}:${thread.id}`}
            thread={thread}
            active={activeThreadID === thread.id}
            menuOpen={openThreadMenuID === `${title}:${thread.id}`}
            onSelect={onSelect}
            onDelete={onDelete}
            onRename={onRename}
            onStarChange={onStarChange}
            onToggleMenu={onToggleMenu}
            onCloseMenu={onCloseMenu}
          />
        ))}
      </div>
    </section>
  );
}

function SidebarThreadItem({
  menuKey,
  thread,
  active,
  menuOpen,
  onSelect,
  onDelete,
  onRename,
  onStarChange,
  onToggleMenu,
  onCloseMenu,
}: {
  menuKey: string;
  thread: Thread;
  active: boolean;
  menuOpen: boolean;
  onSelect(threadID: string): void;
  onDelete(thread: Thread): void;
  onRename(thread: Thread): void;
  onStarChange(thread: Thread, starred: boolean, menuKey: string): void;
  onToggleMenu(menuKey: string): void;
  onCloseMenu(): void;
}) {
  const itemRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!menuOpen) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Node) || itemRef.current?.contains(target)) return;
      onCloseMenu();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [menuOpen, onCloseMenu]);

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
    <div ref={itemRef} className="relative">
      <div className="flex h-7 w-full items-center rounded-md bg-[#10100f] py-0 pl-1.5 pr-1 text-left text-white">
        <button
          className="relative min-w-0 flex-1 overflow-hidden text-left"
          onClick={() => onSelect(thread.id)}
          type="button"
        >
          <span className="block truncate pr-7">{thread.title}</span>
          <span
            className="pointer-events-none absolute inset-y-0 right-0 w-9 bg-gradient-to-r from-transparent to-[#10100f]"
            aria-hidden="true"
          />
        </button>
        <button
          aria-expanded={menuOpen}
          aria-label="Open chat actions"
          className="grid h-6 w-6 shrink-0 place-items-center rounded-md text-[#d8d4ca] transition-colors hover:bg-[#2a2a28] hover:text-white"
          onClick={(event) => {
            event.stopPropagation();
            onToggleMenu(menuKey);
          }}
          type="button"
        >
          <span aria-hidden="true" className="flex h-[10px] flex-col items-center justify-between">
            <span className="h-0.5 w-0.5 rounded-full bg-current" />
            <span className="h-0.5 w-0.5 rounded-full bg-current" />
            <span className="h-0.5 w-0.5 rounded-full bg-current" />
          </span>
        </button>
      </div>
      {menuOpen && (
        <ThreadActionsMenu
          menuKey={menuKey}
          thread={thread}
          onDelete={onDelete}
          onRename={onRename}
          onStarChange={onStarChange}
        />
      )}
    </div>
  );
}

function ThreadActionsMenu({
  menuKey,
  thread,
  onDelete,
  onRename,
  onStarChange,
}: {
  menuKey: string;
  thread: Thread;
  onDelete(thread: Thread): void;
  onRename(thread: Thread): void;
  onStarChange(thread: Thread, starred: boolean, menuKey: string): void;
}) {
  return (
    <div
      aria-label="Chat actions"
      className="absolute left-[174px] z-20 mt-1 w-[168px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] shadow-[0_18px_32px_rgba(0,0,0,0.38)]"
      role="menu"
    >
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8]"
        role="menuitem"
        type="button"
        onClick={() => onStarChange(thread, !thread.starred, menuKey)}
      >
        <span className="w-[18px]" aria-hidden="true">
          {thread.starred ? "★" : "☆"}
        </span>
        {thread.starred ? "Unstar" : "Star"}
      </button>
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8]"
        role="menuitem"
        type="button"
        onClick={() => onRename(thread)}
      >
        <span className="w-[18px]" aria-hidden="true">
          ✎
        </span>
        Rename
      </button>
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8] disabled:cursor-default disabled:opacity-100"
        disabled
        role="menuitem"
        type="button"
      >
        <ProjectMenuIcon />
        Add to project
      </button>
      <div className="mx-[14px] my-[5px] h-px bg-[#77736b]" role="separator" />
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#d98278]"
        role="menuitem"
        type="button"
        onClick={() => onDelete(thread)}
      >
        <TrashMenuIcon />
        Delete
      </button>
    </div>
  );
}

function ProjectMenuIcon() {
  return (
    <svg className="h-[18px] w-[18px] shrink-0" viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path
        d="M4.5 8.5h5l1.6 2h8.4v7.2c0 1.2-.7 1.8-2 1.8h-11c-1.3 0-2-.6-2-1.8V8.5Z"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path
        d="M4.5 8.5V6.8c0-1.1.7-1.7 1.9-1.7h3.1l1.6 2h6.5c1.2 0 1.9.6 1.9 1.7v1.7"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function TrashMenuIcon() {
  return (
    <svg className="-ml-px h-5 w-5 shrink-0" viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path
        d="M8 7.5V6.2c0-.9.6-1.4 1.5-1.4h5c.9 0 1.5.5 1.5 1.4v1.3"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
      />
      <path d="M5.5 7.5h13" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path
        d="M7.2 9.5l.6 8.1c.1 1 .8 1.6 1.8 1.6h4.8c1 0 1.7-.6 1.8-1.6l.6-8.1"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path d="M10.4 11.3v5M13.6 11.3v5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  );
}

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
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <input
          ref={inputRef}
          aria-label="Chat title"
          className="spark-control-text mt-3 h-[38px] w-full rounded-lg border border-[#5b5851] bg-[#1f1f1d] px-3 text-[#f3f0e8] outline-none selection:bg-[#6f6250] selection:text-[#fffaf2]"
          value={title}
          onChange={(event) => onTitleChange(event.target.value)}
        />
        {error !== "" && <ErrorText>{error}</ErrorText>}
        <div className="mt-4 flex justify-end gap-2">
          <button
            className="h-8 rounded-md px-3 text-[#c7c5bd] hover:bg-[#363632]"
            onClick={onCancel}
            type="button"
          >
            Cancel
          </button>
          <button
            className="h-8 rounded-md bg-[#50483d] px-3.5 font-medium text-[#fffaf2] disabled:opacity-50"
            disabled={disabled || title.trim() === ""}
            type="submit"
          >
            Save
          </button>
        </div>
      </form>
    </ModalShell>
  );
}

function DeleteThreadModal({
  error,
  disabled,
  onCancel,
  onDelete,
}: {
  error: string;
  disabled: boolean;
  onCancel(): void;
  onDelete(): void;
}) {
  return (
    <ModalShell title="Delete chat" onCancel={onCancel}>
      <div className="mt-3 text-[13px] leading-5 text-[#d8d4ca]">
        Are you sure you want to delete this chat?
      </div>
      {error !== "" && <ErrorText>{error}</ErrorText>}
      <div className="mt-4 flex justify-end gap-2">
        <button
          autoFocus
          className="h-8 rounded-md px-3 text-[#c7c5bd] hover:bg-[#363632]"
          onClick={onCancel}
          type="button"
        >
          Cancel
        </button>
        <button
          className="h-8 rounded-md bg-[#b85c52] px-3.5 font-medium text-[#fffaf2] disabled:opacity-50"
          disabled={disabled}
          onClick={onDelete}
          type="button"
        >
          Delete
        </button>
      </div>
    </ModalShell>
  );
}

function ModalShell({
  title,
  children,
  onCancel,
}: {
  title: string;
  children: ReactNode;
  onCancel(): void;
}) {
  const titleID = useId();
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onCancel();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onCancel]);
  return (
    <div
      className="fixed inset-0 z-40 grid place-items-center bg-[rgba(10,10,9,0.62)] px-4"
      onClick={(event) => {
        if (event.target === event.currentTarget) onCancel();
      }}
    >
      <div
        aria-labelledby={titleID}
        aria-modal="true"
        className="w-full max-w-[390px] rounded-xl border border-[#4b4a46] bg-[#2a2a28] p-[18px] shadow-[0_28px_70px_rgba(0,0,0,0.55)]"
        role="dialog"
      >
        <h2 id={titleID} className="font-sans text-[22px] font-semibold leading-7 text-[#f3f0e8]">
          {title}
        </h2>
        {children}
      </div>
    </div>
  );
}

function StartPanel({
  displayName,
  draft,
  isSending,
  sendError,
  onDraftChange,
  onSend,
}: {
  displayName: string;
  draft: string;
  isSending: boolean;
  sendError: string;
  onDraftChange(value: string): void;
  onSend(): void;
}) {
  return (
    <section className="flex h-screen min-h-0 flex-col items-center justify-center px-8 pb-[14vh]">
      <h1 className="spark-greeting-text mb-8 flex items-center gap-4 font-serif">
        <img className="h-10 w-10 shrink-0" src={logoImage} alt="" aria-hidden="true" />
        {greetingForNow()}, {displayName}
      </h1>
      <div className="w-full max-w-[674px]">
        <Composer
          variant="start"
          draft={draft}
          disabled={isSending}
          placeholder="How can I help you today?"
          onDraftChange={onDraftChange}
          onSend={onSend}
        />
        {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
        <div className="spark-meta-text mt-4 flex justify-center gap-2 text-[#e8e4da]">
          <PromptChip icon="◇" label="Write" />
          <PromptChip icon="▱" label="Learn" />
          <PromptChip icon="‹/›" label="Code" />
          <PromptChip icon="☕" label="Life stuff" />
          <PromptChip icon="◌" label="Spark's choice" />
        </div>
      </div>
    </section>
  );
}

function ChatPanel({
  thread,
  messages,
  draft,
  streamingText,
  streamingReasoning,
  streamingArtifacts,
  toolEvents,
  sendError,
  isSending,
  mcpStatus,
  onDraftChange,
  onSend,
  onRetry,
}: {
  thread: Thread | null;
  messages: MessageWithToolActivity[];
  draft: string;
  streamingText: string;
  streamingReasoning: string;
  streamingArtifacts: Artifact[];
  toolEvents: ToolActivity[];
  sendError: string;
  isSending: boolean;
  mcpStatus: McpStatusEvent | null;
  onDraftChange(value: string): void;
  onSend(): void;
  onRetry(content: string): void;
}) {
  const transcriptRef = useRef<HTMLDivElement | null>(null);
  const shouldStickToBottomRef = useRef(true);
  const scrollFrameRef = useRef<number | null>(null);
  const [showJumpToBottom, setShowJumpToBottom] = useState(false);
  const showActiveThinkingPanel =
    isSending &&
    streamingText === "" &&
    sendError === "";
  const showStreamingThinkingPanel = showActiveThinkingPanel || streamingReasoning.trim() !== "";

  const refreshScrollState = useCallback(() => {
    const transcript = transcriptRef.current;
    if (transcript === null) return;
    const isAtBottom = isNearBottom(transcript);
    shouldStickToBottomRef.current = isAtBottom;
    setShowJumpToBottom(!isAtBottom);
  }, []);

  const scrollToLatest = useCallback(() => {
    const transcript = transcriptRef.current;
    if (transcript === null) return;
    const scroll = () => {
      transcript.scrollTop = transcript.scrollHeight;
    };
    scroll();
    if (scrollFrameRef.current !== null) window.cancelAnimationFrame(scrollFrameRef.current);
    scrollFrameRef.current = window.requestAnimationFrame(() => {
      scrollFrameRef.current = null;
      scroll();
    });
    shouldStickToBottomRef.current = true;
    setShowJumpToBottom(false);
  }, []);

  const pinToLatest = useCallback(() => {
    shouldStickToBottomRef.current = true;
    setShowJumpToBottom(false);
    scrollToLatest();
  }, [scrollToLatest]);

  const handleSendRequest = useCallback(() => {
    pinToLatest();
    onSend();
  }, [onSend, pinToLatest]);

  const handleRetryRequest = useCallback(
    (content: string) => {
      pinToLatest();
      onRetry(content);
    },
    [onRetry, pinToLatest],
  );

  useLayoutEffect(() => {
    shouldStickToBottomRef.current = true;
    setShowJumpToBottom(false);
    scrollToLatest();
  }, [scrollToLatest, thread?.id]);

  useLayoutEffect(() => {
    if (shouldStickToBottomRef.current) {
      scrollToLatest();
      return;
    }
    refreshScrollState();
  }, [
    messages.length,
    refreshScrollState,
    scrollToLatest,
    sendError,
    showStreamingThinkingPanel,
    streamingArtifacts.length,
    streamingReasoning,
    streamingText,
    toolEvents.length,
  ]);

  useEffect(() => {
    return () => {
      if (scrollFrameRef.current !== null) window.cancelAnimationFrame(scrollFrameRef.current);
    };
  }, []);

  return (
    <section className="flex h-screen min-h-0 flex-col">
      <header
        aria-label="Chat header"
        className="spark-control-text flex h-9 shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 text-[#d5d2c9]"
        role="banner"
      >
        <h1 className="min-w-0 max-w-[28ch] truncate font-sans font-normal sm:max-w-[48ch]">
          {thread?.title ?? "New chat"}
          <span className="ml-2 text-[#88857d]" aria-hidden="true">
            ⌄
          </span>
        </h1>
        {mcpStatus !== null && mcpStatus.configured > 0 && (
          <McpStatusIndicator compact status={mcpStatus} />
        )}
      </header>
      <div className="relative min-h-0 flex-1">
        <div
          ref={transcriptRef}
          aria-label="Conversation transcript"
          className="flex h-full flex-col overflow-y-auto px-6 pt-10 [scrollbar-gutter:stable_both-edges] md:px-8"
          onScroll={refreshScrollState}
          role="region"
        >
          <div className="spark-chat-rail mx-auto w-full max-w-[720px] flex-1 space-y-6 pb-8">
            {messages.map((message, index) => (
              <div key={message.id} className="space-y-6">
                {message.role === "assistant" && message.reasoningContent && (
                  <ThinkingPanel content={message.reasoningContent} complete={true} />
                )}
                {message.role === "assistant" && message.toolEvents !== undefined && (
                  <ToolActivityPanel events={message.toolEvents} />
                )}
                <MessageBubble
                  message={message}
                  retryContent={message.role === "assistant" ? previousUserContent(messages, index) : null}
                  onRetry={handleRetryRequest}
                />
              </div>
            ))}
            {showStreamingThinkingPanel && (
              <ThinkingPanel
                active={showActiveThinkingPanel}
                content={streamingReasoning}
                complete={streamingText !== ""}
              />
            )}
            {toolEvents.length > 0 && <ToolActivityPanel events={toolEvents} />}
            {streamingArtifacts.map((artifact) => (
              <GeneratedArtifactCard key={artifact.id} artifact={artifact} />
            ))}
            {streamingText !== "" && <AssistantText>{streamingText}</AssistantText>}
            {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
          </div>
          <div
            aria-label="Message composer dock"
            className="pointer-events-none sticky bottom-0 -mx-6 bg-bg px-6 pb-5 pt-4 md:-mx-8 md:px-8"
          >
            <div className="pointer-events-none absolute inset-x-0 bottom-full h-8 bg-gradient-to-t from-bg to-transparent" />
            <div className="spark-chat-rail pointer-events-auto mx-auto w-full max-w-[754px]">
              <Composer
                variant="chat"
                draft={draft}
                disabled={isSending}
                placeholder="Write a message..."
                onDraftChange={onDraftChange}
                onSend={handleSendRequest}
              />
              <div className="spark-meta-text mt-2 text-center text-[#858178]">
                Spark can make mistakes. Please double-check responses.
              </div>
            </div>
          </div>
        </div>
        {showJumpToBottom && (
          <button
            aria-label="Jump to latest message"
            className="absolute bottom-40 left-1/2 grid h-9 w-9 -translate-x-1/2 place-items-center rounded-full border border-[#4b4a46] bg-[#2a2a28] text-[#f3f0e8] shadow-[0_10px_24px_rgba(0,0,0,0.35)] transition-colors hover:bg-[#343432]"
            onClick={scrollToLatest}
            title="Jump to latest"
            type="button"
          >
            <svg aria-hidden="true" className="h-4 w-4" fill="none" viewBox="0 0 24 24">
              <path
                d="M12 5v14M6.5 13.5 12 19l5.5-5.5"
                stroke="currentColor"
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2.2"
              />
            </svg>
          </button>
        )}
      </div>
    </section>
  );
}

function ThinkingPanel({
  active = false,
  content,
  complete,
}: {
  active?: boolean;
  content: string;
  complete: boolean;
}) {
  const [expanded, setExpanded] = useState(false);
  const trimmed = content.trim();
  if (trimmed === "" && !active) return null;
  return (
    <div
      aria-label={active ? "Spark is thinking" : undefined}
      aria-live={active ? "polite" : undefined}
      className="spark-thinking-panel"
      role={active ? "status" : undefined}
    >
      <button
        aria-expanded={expanded}
        aria-label={expanded ? "Hide thinking" : "Show thinking"}
        className="spark-thinking-panel-toggle"
        type="button"
        onClick={() => setExpanded((current) => !current)}
      >
        <span className="spark-thinking-panel-label">
          <span className={complete ? "spark-thinking-status-complete" : "spark-thinking-status-active"} aria-hidden="true" />
          {active ? (
            <span className="spark-thinking-label-active" data-text="Thinking">
              Thinking
            </span>
          ) : (
            <span>Thinking</span>
          )}
        </span>
        <span aria-hidden="true" className={expanded ? "spark-thinking-chevron-expanded" : "spark-thinking-chevron"} />
      </button>
      {expanded && (
        <div className="spark-thinking-panel-body">
          <Markdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>
            {trimmed}
          </Markdown>
        </div>
      )}
    </div>
  );
}

function isNearBottom(element: HTMLElement): boolean {
  return element.scrollHeight - element.scrollTop - element.clientHeight <= 48;
}

function previousUserContent(messages: Message[], beforeIndex: number): string | null {
  for (let index = beforeIndex - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (message.role === "user") return message.content;
  }
  return null;
}

function Composer({
  variant,
  draft,
  disabled,
  placeholder,
  onDraftChange,
  onSend,
}: {
  variant: "start" | "chat";
  draft: string;
  disabled: boolean;
  placeholder: string;
  onDraftChange(value: string): void;
  onSend(): void;
}) {
  const height = variant === "start" ? "h-[122px]" : "h-[102px]";
  const sendIconClass = variant === "chat" ? "h-4 w-4 -translate-y-px" : "h-4 w-4";
  const padX = "px-6";
  return (
    <form
      className={`spark-composer ${height} relative rounded-[20px] border border-[#4b4a46] bg-[#2a2a28] shadow-[0_14px_24px_rgba(0,0,0,0.22)]`}
      onSubmit={(event) => {
        event.preventDefault();
        onSend();
      }}
    >
      <textarea
        className={`spark-composer-text h-full w-full resize-none overflow-hidden bg-transparent ${padX} pb-14 pt-5 text-[#f3f0e8] outline-none placeholder:text-[#aaa79e]`}
        placeholder={placeholder}
        value={draft}
        onChange={(event) => onDraftChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.shiftKey) {
            event.preventDefault();
            onSend();
          }
        }}
      />
      <div className={`absolute inset-x-0 bottom-0 flex h-11 items-center justify-between ${padX} text-[#d8d4ca]`}>
        <button className="text-2xl leading-none" type="button" aria-label="Add attachment">
          +
        </button>
        <div className="spark-meta-text flex items-center text-[#d8d4ca]">
          <button
            className="spark-composer-send grid h-7 w-7 place-items-center rounded-md bg-accent text-[#eeeae2] transition-colors hover:bg-accent-strong disabled:cursor-not-allowed disabled:bg-accent disabled:opacity-45"
            disabled={disabled || draft.trim() === ""}
            type="submit"
            aria-label="Send message"
          >
            <svg className={sendIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="none">
              <path d="M12 19V5M6.5 10.5 12 5l5.5 5.5" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        </div>
      </div>
    </form>
  );
}

function PromptChip({ icon, label }: { icon: string; label: string }) {
  return (
    <button className="spark-meta-text flex h-8 items-center gap-1.5 rounded-lg bg-[#3a3a37] px-3 text-[#eeeae2]" type="button">
      <span className="text-[#aaa79e]">{icon}</span>
      {label}
    </button>
  );
}

function toolStatusMeta(event: ToolActivity): { label: string; className: string } {
  if (event.status !== "done") {
    return { label: "Running", className: "bg-[#363632] text-[#c7c5bd]" };
  }
  if (event.content?.startsWith("tool failed")) {
    return { label: "Failed", className: "bg-[#b85c52] text-[#fffaf2]" };
  }
  return { label: "Done", className: "bg-[#363632] text-[#c7c5bd]" };
}

function ToolActivityPanel({ events }: { events: ToolActivity[] }) {
  const [open, setOpen] = useState(false);
  const hasAnyOutput = events.some(
    (event) => event.status === "done" && (event.content?.trim().length ?? 0) > 0,
  );
  return (
    <div className="spark-meta-text max-w-3xl overflow-hidden rounded-lg border border-[#3e3d39] bg-[#282826] text-[#aaa79e]">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-4 py-2.5 text-left disabled:cursor-default"
        onClick={hasAnyOutput ? () => setOpen((value) => !value) : undefined}
        disabled={!hasAnyOutput}
        aria-expanded={hasAnyOutput ? open : undefined}
      >
        {hasAnyOutput ? (
          <svg
            viewBox="0 0 16 16"
            aria-hidden="true"
            className={`size-3 shrink-0 text-[#88857d] transition-transform ${open ? "rotate-90" : ""}`}
          >
            <path d="M6 4l4 4-4 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        ) : (
          <span className="size-3 shrink-0" aria-hidden="true" />
        )}
        <span className="font-medium text-[#f3f0e8]">Tools</span>
        <span className="rounded-full bg-[#363632] px-1.5 text-[11px] text-[#c7c5bd]">{events.length}</span>
      </button>
      <div className="space-y-2 border-t border-[#3e3d39] px-4 py-2.5">
        {events.map((event) => {
          const status = toolStatusMeta(event);
          const output = event.status === "done" ? event.content ?? "" : "";
          return (
            <div key={event.id}>
              <div className="flex items-center justify-between gap-3">
                <span className="min-w-0 truncate text-[#d6d3ca]">{event.name}</span>
                <span className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] ${status.className}`}>{status.label}</span>
              </div>
              {open && output.trim() !== "" && (
                <pre className="mt-1.5 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded-md bg-[#1f1f1d] px-3 py-2 text-xs leading-relaxed text-[#aaa79e]">
                  {output}
                </pre>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function MessageBubble({
  message,
  retryContent,
  onRetry,
}: {
  message: Message;
  retryContent: string | null;
  onRetry(content: string): void;
}) {
  if (message.role === "user") {
    return (
      <div className="spark-user-message group ml-auto w-fit max-w-full md:max-w-[38.25rem]">
        <div className="spark-message-text spark-user-message-text rounded-xl bg-[#111110] px-4 py-3 text-[#f3f0e8]">
          {message.content}
        </div>
        <MessageActions
          copyLabel="Copy message"
          copyText={message.content}
          retryLabel="Retry message"
          onRetry={() => onRetry(message.content)}
          alignRight
        />
      </div>
    );
  }
  return (
    <div className="max-w-[46rem] space-y-3">
      <AssistantText metricsMessage={message} onRetry={retryContent === null ? undefined : () => onRetry(retryContent)}>
        {message.content}
      </AssistantText>
      {message.artifacts?.map((artifact) => (
        <GeneratedArtifactCard key={artifact.id} artifact={artifact} />
      ))}
    </div>
  );
}

function CodeBlock({ children, node: _node, ...props }: ComponentPropsWithoutRef<"pre"> & ExtraProps) {
  const preRef = useRef<HTMLPreElement | null>(null);
  const [copied, setCopied] = useState(false);
  const resetRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (resetRef.current !== null) window.clearTimeout(resetRef.current);
    };
  }, []);

  const handleCopy = useCallback(() => {
    const code = preRef.current?.textContent ?? "";
    void copyResponse(code);
    setCopied(true);
    if (resetRef.current !== null) window.clearTimeout(resetRef.current);
    resetRef.current = window.setTimeout(() => setCopied(false), 1500);
  }, []);

  return (
    <div className="spark-codeblock">
      <button
        type="button"
        className="spark-codeblock-copy"
        onClick={handleCopy}
        aria-label={copied ? "Kopiert" : "Code kopieren"}
        title={copied ? "Kopiert" : "Code kopieren"}
      >
        {copied ? <CheckIcon className="h-4 w-4" /> : <CopyIcon className="h-4 w-4" />}
      </button>
      <pre ref={preRef} {...props}>
        {children}
      </pre>
    </div>
  );
}

export function ProseMarkdown({ children }: { children: string }) {
  return (
    <div className="spark-message-text spark-markdown text-[#f3f0e8]">
      <Markdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          a({ children, ...props }) {
            return (
              <a {...props} target="_blank" rel="noreferrer">
                {children}
              </a>
            );
          },
          pre: CodeBlock,
        }}
      >
        {children}
      </Markdown>
    </div>
  );
}

function AssistantText({
  children,
  onRetry,
  metricsMessage,
}: {
  children: string;
  onRetry?: () => void;
  metricsMessage?: Message;
}) {
  const downloadable = downloadableResponse(children);

  if (downloadable !== null) {
    const { artifact, before, after } = downloadable;
    if (before === "" && after === "") {
      return <DownloadResponseBubble artifact={artifact} />;
    }
    return (
      <div className="spark-assistant-message group w-full space-y-3">
        {before !== "" && <ProseMarkdown>{before}</ProseMarkdown>}
        <DownloadResponseBubble artifact={artifact} />
        {after !== "" && <ProseMarkdown>{after}</ProseMarkdown>}
        <MessageActions
          copyLabel="Copy response"
          copyText={markdownToPlainText(children)}
          retryLabel="Retry response"
          onRetry={onRetry}
          metricsMessage={metricsMessage}
          speakable
        />
      </div>
    );
  }

  const pendingArtifact = pendingFencedArtifact(children);
  if (pendingArtifact !== null) {
    const { before, label, receivedBytes } = pendingArtifact;
    if (before === "") {
      return <PendingDownloadResponseBubble label={label} receivedBytes={receivedBytes} />;
    }
    return (
      <div className="spark-assistant-message group w-full space-y-3">
        <ProseMarkdown>{before}</ProseMarkdown>
        <PendingDownloadResponseBubble label={label} receivedBytes={receivedBytes} />
      </div>
    );
  }

  return (
    <div className="spark-assistant-message group w-full">
      <ProseMarkdown>{children}</ProseMarkdown>
      <MessageActions
        copyLabel="Copy response"
        copyText={markdownToPlainText(children)}
        retryLabel="Retry response"
        onRetry={onRetry}
        metricsMessage={metricsMessage}
        speakable
      />
    </div>
  );
}

function MessageActions({
  copyLabel,
  copyText,
  retryLabel,
  onRetry,
  metricsMessage,
  speakable = false,
  alignRight = false,
}: {
  copyLabel: string;
  copyText: string;
  retryLabel: string;
  onRetry?: () => void;
  metricsMessage?: Message;
  speakable?: boolean;
  alignRight?: boolean;
}) {
  const [copied, setCopied] = useState(false);
  const [speaking, setSpeaking] = useState(false);
  const speakingRef = useRef(false);
  speakingRef.current = speaking;

  // Stop any in-progress narration started here when the bubble unmounts.
  useEffect(() => () => void (speakingRef.current && window.speechSynthesis?.cancel()), []);

  async function handleCopy() {
    await copyResponse(copyText);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1200);
  }

  function endSpeech() {
    window.speechSynthesis?.cancel();
    setSpeaking(false);
  }

  // Replicates AnythingLLM's native TTS exactly (it works reliably in Safari):
  // guard on the engine's own `speaking` flag, no cancel()/resume() before speak,
  // attach an `end` listener, then speak and flag speaking.
  function handleSpeak() {
    const synth = window.speechSynthesis;
    if (!synth) return;
    // Pausing this message while it speaks ends it; if another message is
    // speaking, ignore the click until that one is paused.
    if (synth.speaking && speakingRef.current) {
      endSpeech();
      return;
    }
    if (synth.speaking && !speakingRef.current) return;
    const utterance = new SpeechSynthesisUtterance(copyText);
    utterance.addEventListener("end", endSpeech);
    synth.speak(utterance);
    setSpeaking(true);
  }

  return (
    <div className={`mt-2 flex items-center gap-1 ${alignRight ? "justify-end" : ""}`}>
      {speakable && (
        <button
          className={`grid h-6 w-6 place-items-center transition-colors hover:text-[#f3f0e8] ${
            speaking ? "text-[#f3f0e8]" : "text-[#c7c5bd]"
          }`}
          onClick={handleSpeak}
          type="button"
          title={speaking ? "Stop" : "Read aloud"}
          aria-label={speaking ? "Stop reading" : "Read aloud"}
        >
          <SpeakerIcon />
        </button>
      )}
      <button
        className="grid h-6 w-6 place-items-center text-[#c7c5bd] hover:text-[#f3f0e8]"
        onClick={handleCopy}
        type="button"
        title="Copy"
        aria-label={copyLabel}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </button>
      {onRetry !== undefined && (
        <button
          className="grid h-6 w-6 place-items-center text-[#c7c5bd] hover:text-[#f3f0e8]"
          onClick={onRetry}
          type="button"
          title="Retry"
          aria-label={retryLabel}
        >
          <RetryIcon />
        </button>
      )}
      {metricsMessage && <MessageMetrics message={metricsMessage} />}
    </div>
  );
}

function PendingDownloadResponseBubble({ label, receivedBytes }: { label: string; receivedBytes: number }) {
  const progressText =
    receivedBytes > 0 ? `Receiving file... ${formatReceivedKB(receivedBytes)} received` : "Receiving file...";
  return (
    <div className="max-w-[26rem] rounded-lg border border-[#3e3d39] bg-[#282826] px-4 py-3 text-[#f3f0e8]">
      <div className="flex items-center gap-3">
        <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
          <FileIcon />
        </div>
        <div className="min-w-0 flex-1">
          <div className="spark-message-text truncate">{label} response</div>
          <div className="spark-meta-text text-[#aaa79e]">{progressText}</div>
        </div>
      </div>
    </div>
  );
}

type DownloadableResponse = {
  extension: string;
  label: string;
  mimeType: string;
  content: BlobPart;
};

function DownloadResponseBubble({ artifact }: { artifact: DownloadableResponse }) {
  return (
    <div className="max-w-[26rem] rounded-lg border border-[#3e3d39] bg-[#282826] px-4 py-3 text-[#f3f0e8]">
      <div className="flex items-center gap-3">
        <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
          <FileIcon />
        </div>
        <div className="min-w-0 flex-1">
          <div className="spark-message-text truncate">{artifact.label} response</div>
          <div className="spark-meta-text text-[#aaa79e]">Ready to download</div>
        </div>
        <button
          className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd] transition-colors hover:bg-[#454540] hover:text-[#f3f0e8]"
          onClick={() => downloadEmbeddedArtifact(artifact)}
          type="button"
          title={`Download ${artifact.label} response`}
          aria-label={`Download ${artifact.label} response`}
        >
          <DownloadIcon />
        </button>
      </div>
    </div>
  );
}

function GeneratedArtifactCard({ artifact }: { artifact: Artifact }) {
  const [error, setError] = useState("");

  async function handleDownload() {
    setError("");
    try {
      const blob = await downloadArtifact(artifact.downloadUrl);
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = artifact.displayFilename;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    } catch {
      setError("Download failed");
    }
  }

  return (
    <div className="max-w-[26rem] rounded-lg border border-[#3e3d39] bg-[#282826] px-4 py-3 text-[#f3f0e8]">
      <div className="flex items-center gap-3">
        <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
          <FileIcon />
        </div>
        <div className="min-w-0 flex-1">
          <div className="spark-message-text truncate">{artifact.displayFilename}</div>
          <div className="spark-meta-text text-[#aaa79e]">
            {artifact.mimeType} · {formatFileSize(artifact.sizeBytes)}
          </div>
          {error !== "" && <div className="spark-meta-text text-[#d36f67]">{error}</div>}
        </div>
        <button
          className="grid h-8 w-8 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd] transition-colors hover:bg-[#454540] hover:text-[#f3f0e8]"
          onClick={handleDownload}
          type="button"
          title={`Download ${artifact.displayFilename}`}
          aria-label={`Download ${artifact.displayFilename}`}
        >
          <DownloadIcon />
        </button>
      </div>
    </div>
  );
}

async function copyResponse(content: string) {
  await navigator.clipboard?.writeText(content);
}

function downloadEmbeddedArtifact(artifact: DownloadableResponse) {
  const url = URL.createObjectURL(new Blob([artifact.content], { type: artifact.mimeType }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `spark-response.${artifact.extension}`;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

type EmbeddedArtifact = {
  artifact: DownloadableResponse;
  before: string;
  after: string;
};

type PendingArtifact = {
  label: string;
  before: string;
  receivedBytes: number;
};

function downloadableResponse(content: string): EmbeddedArtifact | null {
  const dataURL = dataURLArtifact(content);
  if (dataURL !== null) return { artifact: dataURL, before: "", after: "" };

  return fencedArtifact(content);
}

function pendingFencedArtifact(content: string): PendingArtifact | null {
  const matches = [...content.matchAll(/(?:^|\n)```([a-z0-9_-]+)[ \t]*\n/gi)];
  if (matches.length !== 1) return null;

  const match = matches[0];
  const extension = extensionByLanguage.get(match[1].trim().toLowerCase());
  if (extension === undefined) return null;

  const start = match.index ?? 0;
  const artifactStart = start + match[0].length;
  if (content.slice(artifactStart).includes("\n```")) return null;

  return {
    label: extension.toUpperCase(),
    before: content.slice(0, start).trim(),
    receivedBytes: utf8ByteLength(content.slice(artifactStart)),
  };
}

function utf8ByteLength(content: string): number {
  return new TextEncoder().encode(content).length;
}

function formatReceivedKB(bytes: number): string {
  const kb = bytes / 1024;
  const rounded = kb >= 10 ? Math.round(kb).toString() : kb.toFixed(1);
  return `${rounded} KB`;
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb >= 10 ? Math.round(kb).toString() : kb.toFixed(1)} KB`;
  const mb = kb / 1024;
  return `${mb >= 10 ? Math.round(mb).toString() : mb.toFixed(1)} MB`;
}

function fencedArtifact(content: string): EmbeddedArtifact | null {
  const matches = [...content.matchAll(/(?:^|\n)```([a-z0-9_-]+)[ \t]*\n([\s\S]*?)\n```(?=\n|$)/gi)];
  const downloadable = matches.flatMap((match) => {
    const extension = extensionByLanguage.get(match[1].trim().toLowerCase());
    return extension === undefined ? [] : [{ match, extension }];
  });

  if (downloadable.length !== 1) return null;

  const { match, extension } = downloadable[0];
  const start = match.index ?? 0;
  return {
    artifact: {
      extension,
      label: extension.toUpperCase(),
      mimeType: DOWNLOAD_FORMATS[extension].mimeType,
      content: match[2],
    },
    before: content.slice(0, start).trim(),
    after: content.slice(start + match[0].length).trim(),
  };
}

// Single source of truth for downloadable artifact formats. Keyed by file
// extension; `languages` are the fenced code-block tags and `mimeTypes` the
// data: URL types that map onto each format. New format = one entry here.
type DownloadFormat = { mimeType: string; languages: string[]; mimeTypes: string[] };

const DOWNLOAD_FORMATS: Record<string, DownloadFormat> = {
  csv: { mimeType: "text/csv;charset=utf-8", languages: ["csv"], mimeTypes: ["text/csv"] },
  html: { mimeType: "text/html;charset=utf-8", languages: ["html"], mimeTypes: ["text/html"] },
  json: { mimeType: "application/json;charset=utf-8", languages: ["json"], mimeTypes: ["application/json"] },
  svg: { mimeType: "application/xml;charset=utf-8", languages: ["svg"], mimeTypes: ["image/svg+xml"] },
  xml: { mimeType: "application/xml;charset=utf-8", languages: ["xml"], mimeTypes: ["application/xml", "text/xml"] },
  pptx: {
    mimeType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    languages: [],
    mimeTypes: ["application/vnd.openxmlformats-officedocument.presentationml.presentation"],
  },
  xlsx: {
    mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    languages: [],
    mimeTypes: ["application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"],
  },
};

const extensionByLanguage = new Map<string, string>(
  Object.entries(DOWNLOAD_FORMATS).flatMap(([extension, format]) =>
    format.languages.map((language) => [language, extension] as const),
  ),
);

const extensionByMimeType = new Map<string, string>(
  Object.entries(DOWNLOAD_FORMATS).flatMap(([extension, format]) =>
    format.mimeTypes.map((mimeType) => [mimeType, extension] as const),
  ),
);

function dataURLArtifact(content: string): DownloadableResponse | null {
  const match = content.trim().match(/^data:([^;,]+)(;base64)?,([\s\S]+)$/i);
  if (match === null) return null;
  const mimeType = match[1].toLowerCase();
  const extension = extensionByMimeType.get(mimeType);
  if (extension === undefined) return null;
  const encoded = match[3];
  let artifactContent: BlobPart;
  try {
    artifactContent = match[2]
      ? Uint8Array.from(atob(encoded), (character) => character.charCodeAt(0))
      : decodeURIComponent(encoded);
  } catch {
    return null;
  }
  return {
    extension,
    label: extension.toUpperCase(),
    mimeType,
    content: artifactContent,
  };
}

function markdownToPlainText(content: string): string {
  return content
    .replace(/\r\n/g, "\n")
    .replace(/^```[a-z0-9_-]*\n([\s\S]*?)\n```$/gim, "$1")
    .replace(/^#{1,6}\s+/gm, "")
    .replace(/^\s{0,3}>\s?/gm, "")
    .replace(/^\s*[-*+]\s+/gm, "")
    .replace(/^\s*\d+\.\s+/gm, "")
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/(\*\*|__)(.*?)\1/g, "$2")
    .replace(/(\*|_)(.*?)\1/g, "$2")
    .replace(/~~(.*?)~~/g, "$1")
    .replace(/`([^`]+)`/g, "$1")
    .trim();
}

function CopyIcon({ className = "h-[1.33rem] w-[1.33rem]" }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M8 8.5V6.8c0-1 .8-1.8 1.8-1.8h7.4c1 0 1.8.8 1.8 1.8v7.4c0 1-.8 1.8-1.8 1.8h-1.7"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M5.8 8h7.4c1 0 1.8.8 1.8 1.8v7.4c0 1-.8 1.8-1.8 1.8H5.8c-1 0-1.8-.8-1.8-1.8V9.8C4 8.8 4.8 8 5.8 8Z"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function SpeakerIcon() {
  return (
    <svg className="h-[1.33rem] w-[1.33rem]" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M4 9.5h3l4-3.3v11.6l-4-3.3H4z"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path d="M15 9.2a4 4 0 0 1 0 5.6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M17.6 6.6a7.5 7.5 0 0 1 0 10.8" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}

function CheckIcon({ className = "h-[1.33rem] w-[1.33rem]" }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="m5 12.5 4.2 4.2L19 7"
        stroke="currentColor"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function DownloadIcon() {
  return (
    <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M12 4.5v9M7.5 10 12 14.5 16.5 10"
        stroke="currentColor"
        strokeWidth="1.9"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M5 18.5h14"
        stroke="currentColor"
        strokeWidth="1.9"
        strokeLinecap="round"
      />
    </svg>
  );
}

function RetryIcon() {
  return (
    <svg className="h-[1.33rem] w-[1.33rem]" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M18.5 9.2A6.5 6.5 0 1 0 19 12"
        stroke="currentColor"
        strokeWidth="1.9"
        strokeLinecap="round"
      />
      <path
        d="M18.5 5.5v3.7h-3.7"
        stroke="currentColor"
        strokeWidth="1.9"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function FileIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M7 4.5h6.2L18 9.3v8.9c0 1-.8 1.8-1.8 1.8H7.8c-1 0-1.8-.8-1.8-1.8V6.3c0-1 .8-1.8 1.8-1.8Z"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path
        d="M13 4.8V9h4.2"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function ErrorText({ children }: { children: React.ReactNode }) {
  return (
    <div className="spark-meta-text mt-3 max-w-3xl rounded-lg border border-accent bg-[#282826] px-4 py-3 text-accent">
      {children}
    </div>
  );
}

function upsertToolCall(current: ToolActivity[], event: ToolCallEvent): ToolActivity[] {
  const next = current.filter((item) => item.id !== event.id);
  return [...next, { id: event.id, name: event.name, status: "running" }];
}

function upsertToolResult(current: ToolActivity[], event: ToolResultEvent): ToolActivity[] {
  return current.map((item) =>
    item.id === event.id ? { ...item, status: "done", content: event.content } : item,
  );
}
