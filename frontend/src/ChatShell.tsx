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
  stopMessage,
  streamMessage,
  type Artifact,
  type McpStatusEvent,
  type Message,
  type Project,
  type Thread,
  type User,
} from "./api";
import {
  appendReasoningDelta,
  completeTrace,
  faviconURL,
  normalizeActivityTrace,
  summarizeTrace,
  upsertTraceToolCall,
  upsertTraceToolResult,
  type ActivityTraceEvent,
  type ActivityTraceToolEvent,
} from "./activityTrace";
import logoImage from "./assets/sloppy.png";
import { MessageMetrics } from "./MessageMetrics";
import { formatDuration } from "./metrics";
import { ThreadActionsMenu } from "./ThreadActionsMenu";
import { ChatsPage } from "./ChatsPage";

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
  | { view: "chats" }
  | { view: "chat"; threadID: string };

type MessageWithActivityTrace = Message & {
  activityTrace?: ActivityTraceEvent[];
  activityTraceInitiallyExpanded?: boolean;
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
  const [messages, setMessages] = useState<MessageWithActivityTrace[]>([]);
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
  const [streamingArtifacts, setStreamingArtifacts] = useState<Artifact[]>([]);
  const [activityTrace, setActivityTrace] = useState<ActivityTraceEvent[]>([]);
  const [mcpStatus, setMcpStatus] = useState<McpStatusEvent | null>(null);
  const [sendError, setSendError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [isUpdatingStar, setIsUpdatingStar] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [threadMutationVersion, setThreadMutationVersion] = useState(0);
  const activeThreadIDRef = useRef<string | null>(null);
  const streamAbortRef = useRef<AbortController | null>(null);
  const activityTraceRef = useRef<ActivityTraceEvent[]>([]);
  const activeActivityTraceExpandedRef = useRef(false);

  const updateActivityTrace = useCallback((updater: (current: ActivityTraceEvent[]) => ActivityTraceEvent[]) => {
    const next = updater(activityTraceRef.current);
    activityTraceRef.current = next;
    setActivityTrace(next);
  }, []);

  const clearActivityTrace = useCallback(() => {
    activityTraceRef.current = [];
    setActivityTrace([]);
  }, []);

  const handleActionError = useCallback(
    (error: unknown, fallback: string, setError: (message: string) => void) => {
      if (error instanceof AuthExpiredError) {
        onSessionExpired();
        return;
      }
      setError(error instanceof Error && error.message !== "" ? error.message : fallback);
    },
    [onSessionExpired],
  );

  const handleStopResponse = useCallback(() => {
    if (!isSending) return;
    const threadID = activeThreadIDRef.current;
    if (threadID !== null) {
      void stopMessage(threadID).catch((error: unknown) => {
        handleActionError(error, "Message failed to stop.", setSendError);
      });
    }
    streamAbortRef.current?.abort();
  }, [handleActionError, isSending]);

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
    if (!isSending) return;
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      event.preventDefault();
      handleStopResponse();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [handleStopResponse, isSending]);

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
      clearActivityTrace();
      setSendError("");
      return;
    }
    if (activeThreadIDRef.current === route.threadID) return;
    let active = true;
    streamAbortRef.current?.abort();
    // Drop any activity trace left over from the previous thread (e.g. a failed
    // turn whose trace is now kept) before this thread's transcript loads.
    clearActivityTrace();
    getThread(route.threadID)
      .then((response) => {
        if (!active) return;
        setActiveThread(response.thread);
        activeThreadIDRef.current = response.thread.id;
        setMessages(response.messages.map(withNormalizedActivityTrace));
        setStreamingText("");
        setStreamingArtifacts([]);
        clearActivityTrace();
        setSendError("");
      })
      .catch((error: unknown) => {
        if (!active) return;
        handleActionError(error, "Chat failed to load.", setLoadError);
      });
    return () => {
      active = false;
    };
  }, [clearActivityTrace, handleActionError, route]);

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
    clearActivityTrace();
    activeActivityTraceExpandedRef.current = false;
    setSendError("");
    navigate({ view: "new" });
    setRoute({ view: "new" });
  }, [clearActivityTrace, onChat]);

  const navigateToChats = useCallback(() => {
    onChat();
    navigate({ view: "chats" });
    setRoute({ view: "chats" });
  }, [onChat]);

  const reloadThreads = useCallback(() => {
    listThreads({ limit: 30 })
      .then((nextThreads) => setThreads(nextThreads))
      .catch((error: unknown) => {
        if (error instanceof AuthExpiredError) onSessionExpired();
      });
  }, [onSessionExpired]);

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
      setThreadMutationVersion((value) => value + 1);
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
      setThreadMutationVersion((value) => value + 1);
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
      setThreadMutationVersion((value) => value + 1);
      if (activeThreadIDRef.current === deletingThread.id) {
        streamAbortRef.current?.abort();
        activeThreadIDRef.current = null;
        setActiveThread(null);
        setMessages([]);
        setStreamingText("");
        setStreamingArtifacts([]);
        clearActivityTrace();
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
    setStreamingArtifacts([]);
    clearActivityTrace();
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
          if (!isCurrentThread()) return;
          updateActivityTrace((current) => appendReasoningDelta(current, delta));
        },
        onToolCall: (event) => {
          if (!isCurrentThread()) return;
          updateActivityTrace((current) => upsertTraceToolCall(current, event));
        },
        onToolResult: (event) => {
          if (!isCurrentThread()) return;
          updateActivityTrace((current) => upsertTraceToolResult(current, event));
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
          const completedTrace = completeTrace(activityTraceRef.current);
          setMessages((current) => [
            ...current,
            completedTrace.length > 0
              ? {
                  ...message,
                  activityTrace: completedTrace,
                  activityTraceInitiallyExpanded: activeActivityTraceExpandedRef.current,
                }
              : message,
          ]);
          activeActivityTraceExpandedRef.current = false;
          setStreamingText("");
          setStreamingArtifacts([]);
          clearActivityTrace();
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
      setStreamingArtifacts([]);
      // Keep any activity trace visible so a failed turn still shows what was
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
    <div
      className={`grid h-screen bg-bg font-sans text-ink transition-[grid-template-columns] duration-200 ease-out ${
        sidebarCollapsed ? "grid-cols-[56px_1fr]" : "grid-cols-[362px_1fr]"
      }`}
    >
      <aside className="slopr-sidebar-text flex min-h-0 flex-col overflow-hidden border-r border-[#343432] bg-panel pl-0.5 text-[#c7c5bd]">
        <div className={`flex h-11 items-center px-3 ${sidebarCollapsed ? "justify-center" : "justify-between"}`}>
          {!sidebarCollapsed && (
            <div className="slopr-wordmark font-serif font-medium text-[#f4f0e8]">Slopr</div>
          )}
          <div className="flex items-center gap-3 text-[#aaa79e]">
            {!sidebarCollapsed && (
              <button
                type="button"
                aria-label="Search"
                className="grid place-items-center rounded transition-colors hover:text-white"
              >
                <svg className="h-[18px] w-[18px]" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                  <circle cx="11" cy="11" r="6" stroke="currentColor" strokeWidth="1.5" />
                  <path d="m20 20-3.6-3.6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                </svg>
              </button>
            )}
            <button
              type="button"
              aria-label={sidebarCollapsed ? "Show sidebar" : "Hide sidebar"}
              aria-expanded={!sidebarCollapsed}
              onClick={() => setSidebarCollapsed((value) => !value)}
              className="grid place-items-center rounded transition-colors hover:text-white"
            >
              <svg className="h-[18px] w-[18px]" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <rect x="4" y="5" width="16" height="14" rx="2" stroke="currentColor" strokeWidth="1.5" />
                <path d="M9.5 5v14" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
            </button>
          </div>
        </div>
        <nav className="slopr-sidebar-scroll min-h-0 flex-1 overflow-y-auto px-2 pb-4 pt-2">
          <button
            className={`flex h-7 w-full items-center rounded-md px-1.5 text-left transition-colors hover:bg-[#2a2a28] ${
              sidebarCollapsed ? "justify-center" : "gap-2.5"
            } ${route.view === "new" && !showAdmin ? "bg-[#111110]" : ""}`}
            onClick={navigateToNew}
            type="button"
            aria-label="New chat"
          >
            <span className="grid h-[20px] w-[20px] shrink-0 place-items-center rounded-full bg-[hsl(180deg_3%_19%)] text-[hsl(55deg_9%_74%)]">
              <svg className="h-[13px] w-[13px]" viewBox="0 0 24 24" aria-hidden="true" fill="none">
                <path d="M12 4v16M4 12h16" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
              </svg>
            </span>
            {!sidebarCollapsed && <span>New chat</span>}
          </button>
          <SidebarPrimaryItem
            label="Chats"
            icon="chats"
            collapsed={sidebarCollapsed}
            active={route.view === "chats" && !showAdmin}
            onClick={navigateToChats}
          />
          <SidebarPrimaryItem label="Projects" icon="projects" collapsed={sidebarCollapsed} />
          {!sidebarCollapsed && (
            <>
          {loadError !== "" && (
            <div className="slopr-meta-text mx-1.5 mt-3 rounded-md border border-accent px-2 py-2 text-accent">
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
            <div className="slopr-meta-text mb-2 flex items-center justify-between px-1.5 text-[#97958c]">
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
                  className="slopr-sidebar-text w-full rounded-md border border-[#3b3b38] bg-[#20201f] px-2 py-1.5 text-ink outline-none placeholder:text-muted focus:border-[#69665f]"
                  placeholder="Project name"
                  value={projectName}
                  onChange={(event) => setProjectName(event.target.value)}
                />
                <div className="flex gap-2">
                  <button
                    className="slopr-sidebar-text rounded-md bg-[#393936] px-3 py-1.5 font-medium text-white disabled:opacity-50"
                    disabled={projectName.trim() === "" || isCreatingProject}
                    type="submit"
                  >
                    Create
                  </button>
                  <button
                    className="slopr-sidebar-text px-2 py-1.5 text-muted transition-colors hover:text-ink"
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
            </>
          )}
        </nav>
        <div className="border-t border-[#343432] px-3 py-3">
          <div className={`flex items-center ${sidebarCollapsed ? "justify-center" : "gap-3"}`}>
            <div className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-[#dedbd0] text-xs font-semibold text-[#1d1d1b]">
              {initialsFor(displayName)}
            </div>
            {!sidebarCollapsed && (
              <>
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[#f4f0e8]">{displayName}</div>
                  <div className="truncate font-normal text-[#8f8b82]">{user.role}</div>
                </div>
                <button className="rounded-md px-2 py-1 text-[#aaa79e] hover:bg-[#2a2a28]" onClick={onLogout}>
                  Logout
                </button>
              </>
            )}
          </div>
        </div>
      </aside>
      <main className="min-w-0 bg-bg">
        {showAdmin ? (
          adminPanel
        ) : route.view === "chats" ? (
          <ChatsPage
            mutationVersion={threadMutationVersion}
            onNewChat={navigateToNew}
            onSelectThread={(threadID) => void selectThread(threadID)}
            onRenameThread={openRenameModal}
            onDeleteThread={openDeleteModal}
            onStarThread={(thread, starred, menuKey) => void handleSetThreadStarred(thread, starred, menuKey)}
            onAfterBulkDelete={reloadThreads}
            onSessionExpired={onSessionExpired}
          />
        ) : route.view === "new" ? (
          <StartPanel
            displayName={displayName}
            draft={draft}
            isSending={isSending}
            mcpStatus={mcpStatus}
            sendError={sendError}
            onDraftChange={setDraft}
            onSend={handleSend}
            onStop={handleStopResponse}
          />
        ) : (
          <ChatPanel
            thread={activeThread}
        messages={messages}
            draft={draft}
            streamingText={streamingText}
            streamingArtifacts={streamingArtifacts}
            activityTrace={activityTrace}
            sendError={sendError}
            isSending={isSending}
            mcpStatus={mcpStatus}
            openThreadMenuID={openThreadMenuID}
            onDraftChange={setDraft}
            onSend={handleSend}
            onStop={handleStopResponse}
        onRetry={handleRetry}
        onActiveActivityTraceExpandedChange={(expanded) => {
          activeActivityTraceExpandedRef.current = expanded;
        }}
        onDeleteThread={openDeleteModal}
            onRenameThread={openRenameModal}
            onStarThread={(thread, starred, menuKey) => void handleSetThreadStarred(thread, starred, menuKey)}
            onToggleThreadMenu={(menuKey) =>
              setOpenThreadMenuID((current) => (current === menuKey ? null : menuKey))
            }
            onCloseThreadMenu={() => setOpenThreadMenuID(null)}
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
  if (path === "/chats") return { view: "chats" };
  return { view: "new" };
}

function pathForRoute(route: RouteState): string {
  switch (route.view) {
    case "new":
      return "/new";
    case "chats":
      return "/chats";
    case "chat":
      return `/chat/${encodeURIComponent(route.threadID)}`;
  }
}

function navigate(route: RouteState) {
  const path = pathForRoute(route);
  if (window.location.pathname !== path) {
    window.history.pushState({}, "", path);
  }
}

function upsertThread(current: Thread[], thread: Thread): Thread[] {
  return [thread, ...current.filter((item) => item.id !== thread.id)];
}

function withNormalizedActivityTrace(message: Message): MessageWithActivityTrace {
  return {
    ...message,
    activityTrace: normalizeActivityTrace(message.activityTrace),
  };
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

function greetingForNow(fullName: string) {
  const name = fullName.trim().split(/\s+/)[0];
  const hour = new Date().getHours();
  if (hour < 10) return `Morning, ${name}`;
  if (hour >= 18) return `Evening, ${name}`;
  if (hour >= 13) return `Afternoon, ${name}`;
  return `${name} returns!`;
}

function SidebarPrimaryItem({
  icon,
  label,
  collapsed = false,
  active = false,
  onClick,
}: {
  icon: SidebarIconName;
  label: string;
  collapsed?: boolean;
  active?: boolean;
  onClick?(): void;
}) {
  const className = `flex h-7 w-full items-center rounded-md px-1.5 text-left text-[#c7c5bd] ${
    collapsed ? "justify-center" : "gap-2.5"
  } ${active ? "bg-[#111110]" : ""} ${onClick !== undefined ? "transition-colors hover:bg-[#2a2a28]" : ""}`;
  const content = (
    <>
      <SidebarIcon name={icon} />
      {!collapsed && <span className="truncate">{label}</span>}
    </>
  );
  if (onClick === undefined) {
    return <div className={className}>{content}</div>;
  }
  return (
    <button type="button" className={className} onClick={onClick} aria-label={label}>
      {content}
    </button>
  );
}

function SidebarIcon({ name }: { name: SidebarIconName }) {
  const className = "h-[21px] w-[21px] shrink-0 text-[#f0eee7]";
  if (name === "chats") {
    return (
      <svg className={className} viewBox="0 0 24 24" aria-hidden="true" fill="none">
        <path
          d="M6.5 15.5c-2.2-.2-3.5-1.6-3.5-3.8V8.8C3 6.3 4.5 5 7.1 5h5.1c2.6 0 4.1 1.3 4.1 3.8v2.9c0 2.5-1.5 3.8-4.1 3.8H9l-3.3 2.3v-2.3Z"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinejoin="round"
        />
        <path
          d="M17.2 9.2c2.5.1 3.8 1.4 3.8 3.8v2.4c0 2.2-1.3 3.5-3.6 3.7v2l-2.9-2h-3.2c-1.8 0-3-.7-3.5-2"
          stroke="currentColor"
          strokeWidth="1.5"
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
          strokeWidth="1.5"
          strokeLinejoin="round"
        />
        <path d="M4.5 8.5V6.8c0-1.1.7-1.7 1.9-1.7h3.1l1.6 2h6.5c1.2 0 1.9.6 1.9 1.7v1.7" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
      </svg>
    );
  }
  return null;
}

function McpStatusIndicator({ compact = false, status }: { compact?: boolean; status: McpStatusEvent }) {
  const allActive = status.active === status.configured;
  const ringClass = allActive ? "border-success" : "border-danger";
  const dotClass = allActive ? "bg-success" : "bg-danger";
  const inactiveServers = status.servers?.filter((server) => !server.active).map((server) => server.name) ?? [];
  const tooltip =
    !allActive && inactiveServers.length > 0
      ? `${status.active} of ${status.configured} MCP servers active. Failed: ${inactiveServers.join(", ")}`
      : `${status.active} of ${status.configured} MCP servers active`;
  return (
    <div
      className={`slopr-meta-text flex items-center gap-1.5 text-muted ${compact ? "" : "mt-2"}`}
      title={tooltip}
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
      <div className="slopr-meta-text mb-2 px-1.5 text-[#97958c]">{title}</div>
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
          <span className="block whitespace-nowrap pr-7">{thread.title}</span>
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
          className="slopr-control-text mt-3 h-[38px] w-full rounded-lg border border-[#5b5851] bg-[#1f1f1d] px-3 text-[#f3f0e8] outline-none selection:bg-[#6f6250] selection:text-[#fffaf2]"
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
      className="fixed inset-0 z-40 grid place-items-center bg-[rgba(10,10,9,0.62)] pr-4 pl-[378px]"
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
  mcpStatus,
  sendError,
  onDraftChange,
  onSend,
  onStop,
}: {
  displayName: string;
  draft: string;
  isSending: boolean;
  mcpStatus: McpStatusEvent | null;
  sendError: string;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
}) {
  return (
    <section className="flex h-screen min-h-0 flex-col">
      <header
        aria-label="Chat header"
        className="slopr-control-text flex h-9 shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 text-[#d5d2c9]"
        role="banner"
      >
        <h1 className="min-w-0 max-w-[28ch] truncate font-sans font-normal sm:max-w-[48ch]">New chat</h1>
        {mcpStatus !== null && mcpStatus.configured > 0 && (
          <McpStatusIndicator compact status={mcpStatus} />
        )}
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-8 pb-[14vh]">
        <h2 className="slopr-greeting-text mb-8 flex items-center gap-4 font-serif">
          <img className="h-16 w-auto shrink-0 -translate-y-1" src={logoImage} alt="" aria-hidden="true" />
          {greetingForNow(displayName)}
        </h2>
        <div className="w-full max-w-[674px]">
          <Composer
            variant="start"
            draft={draft}
            isSending={isSending}
            placeholder="How can I help you today?"
            onDraftChange={onDraftChange}
            onSend={onSend}
            onStop={onStop}
          />
          {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
          <div className="slopr-meta-text mt-4 flex justify-center gap-2 text-[#e8e4da]">
            <PromptChip icon="◇" label="Write" />
            <PromptChip icon="▱" label="Learn" />
            <PromptChip icon="‹/›" label="Code" />
            <PromptChip icon="☕" label="Life stuff" />
            <PromptChip icon="◌" label="Slopr's choice" />
          </div>
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
  streamingArtifacts,
  activityTrace,
  sendError,
  isSending,
  mcpStatus,
  openThreadMenuID,
  onDraftChange,
  onSend,
  onStop,
  onRetry,
  onActiveActivityTraceExpandedChange,
  onDeleteThread,
  onRenameThread,
  onStarThread,
  onToggleThreadMenu,
  onCloseThreadMenu,
}: {
  thread: Thread | null;
  messages: MessageWithActivityTrace[];
  draft: string;
  streamingText: string;
  streamingArtifacts: Artifact[];
  activityTrace: ActivityTraceEvent[];
  sendError: string;
  isSending: boolean;
  mcpStatus: McpStatusEvent | null;
  openThreadMenuID: string | null;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
  onRetry(content: string): void;
  onActiveActivityTraceExpandedChange(expanded: boolean): void;
  onDeleteThread(thread: Thread): void;
  onRenameThread(thread: Thread): void;
  onStarThread(thread: Thread, starred: boolean, menuKey: string): void;
  onToggleThreadMenu(menuKey: string): void;
  onCloseThreadMenu(): void;
}) {
  const transcriptRef = useRef<HTMLDivElement | null>(null);
  const headerMenuRef = useRef<HTMLDivElement | null>(null);
  const shouldStickToBottomRef = useRef(true);
  const scrollFrameRef = useRef<number | null>(null);
  const [showJumpToBottom, setShowJumpToBottom] = useState(false);
  const headerMenuKey = thread === null ? null : `Header:${thread.id}`;
  const headerMenuOpen = headerMenuKey !== null && openThreadMenuID === headerMenuKey;
  const hasActiveActivityTrace = activityTrace.length > 0;
  const showActiveActivityTrace = hasActiveActivityTrace || (isSending && sendError === "");

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
      // Honour a user scroll that happened between the synchronous scroll above and this
      // frame: if they scrolled away, refreshScrollState cleared the flag, so don't yank
      // them back to the bottom.
      if (shouldStickToBottomRef.current) scroll();
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
    showActiveActivityTrace,
    streamingArtifacts.length,
    streamingText,
    activityTrace.length,
  ]);

  useEffect(() => {
    return () => {
      if (scrollFrameRef.current !== null) window.cancelAnimationFrame(scrollFrameRef.current);
    };
  }, []);

  useEffect(() => {
    if (!headerMenuOpen) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Node) || headerMenuRef.current?.contains(target)) return;
      onCloseThreadMenu();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [headerMenuOpen, onCloseThreadMenu]);

  return (
    <section className="flex h-screen min-h-0 flex-col">
      <header
        aria-label="Chat header"
        className="slopr-control-text flex h-9 shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 text-[#d5d2c9]"
        role="banner"
      >
        <div ref={headerMenuRef} className="relative flex min-w-0 items-center">
          <h1 className="min-w-0 max-w-[28ch] truncate font-sans font-normal sm:max-w-[48ch]">
            {thread?.title ?? "New chat"}
          </h1>
          {thread !== null && headerMenuKey !== null && (
            <button
              aria-expanded={headerMenuOpen}
              aria-label="Open chat actions"
              className="ml-1 grid h-5 w-5 shrink-0 place-items-center rounded-md text-[#88857d] transition-colors hover:bg-[#2a2a28] hover:text-[#f3f0e8]"
              onClick={() => onToggleThreadMenu(headerMenuKey)}
              type="button"
            >
              <span
                aria-hidden="true"
                className={headerMenuOpen ? "slopr-thinking-chevron-expanded" : "slopr-thinking-chevron"}
              />
            </button>
          )}
          {thread !== null && headerMenuKey !== null && headerMenuOpen && (
            <ThreadActionsMenu
              menuKey={headerMenuKey}
              thread={thread}
              className="right-0 top-full"
              onDelete={onDeleteThread}
              onRename={onRenameThread}
              onStarChange={onStarThread}
            />
          )}
        </div>
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
          <div className="slopr-chat-rail mx-auto w-full max-w-[720px] flex-1 space-y-6 pb-8">
            {messages.map((message, index) => (
              <div key={message.id} className="space-y-6">
                {message.role === "assistant" && message.activityTrace !== undefined && (
                  <ActivityTracePanel
                    events={message.activityTrace}
                    active={false}
                    initiallyExpanded={message.activityTraceInitiallyExpanded === true}
                  />
                )}
                {message.role === "assistant" && message.activityTrace === undefined && message.reasoningContent && (
                  <ActivityTracePanel
                    events={[
                      {
                        id: `${message.id}-reasoning`,
                        type: "reasoning",
                        content: message.reasoningContent,
                        status: "done",
                      },
                    ]}
                    active={false}
                  />
                )}
                <MessageBubble
                  message={message}
                  retryContent={message.role === "assistant" ? previousUserContent(messages, index) : null}
                  onRetry={handleRetryRequest}
                />
              </div>
            ))}
            {showActiveActivityTrace && (
              <ActivityTracePanel
                events={activityTrace}
                active={true}
                onExpandedChange={onActiveActivityTraceExpandedChange}
              />
            )}
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
            <div className="slopr-chat-rail pointer-events-auto mx-auto w-full max-w-[754px]">
              <Composer
                variant="chat"
                draft={draft}
                isSending={isSending}
                placeholder="Write a message..."
                onDraftChange={onDraftChange}
                onSend={handleSendRequest}
                onStop={onStop}
              />
              <div className="slopr-meta-text mt-2 text-center text-[#858178]">
                Slopr can make mistakes. Please double-check responses.
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

function ActivityTracePanel({
  events,
  active,
  initiallyExpanded = false,
  onExpandedChange,
}: {
  events: ActivityTraceEvent[];
  active: boolean;
  initiallyExpanded?: boolean;
  onExpandedChange?(expanded: boolean): void;
}) {
  const [expanded, setExpanded] = useState(initiallyExpanded);
  if (events.length === 0 && !active) return null;
  const summary = active ? "Thinking" : summarizeTrace(events);
  return (
    <div
      aria-label={active ? "Slopr activity trace" : undefined}
      aria-live={active ? "polite" : undefined}
      className="slopr-activity-trace"
      role={active ? "status" : undefined}
    >
      <button
        aria-expanded={expanded}
        aria-label={expanded ? "Hide activity" : "Show activity"}
        className="slopr-activity-trace-toggle"
        type="button"
        onClick={() =>
          setExpanded((current) => {
            const next = !current;
            onExpandedChange?.(next);
            return next;
          })
        }
      >
        <span className="slopr-activity-trace-label">
          <span className={active ? "slopr-thinking-status-active" : "slopr-thinking-status-complete"} aria-hidden="true" />
          {active ? (
            <span className="slopr-thinking-label-active" data-text="Thinking">
              Thinking
            </span>
          ) : (
            <span>{summary}</span>
          )}
          <span aria-hidden="true" className={expanded ? "slopr-thinking-chevron-expanded" : "slopr-thinking-chevron"} />
        </span>
      </button>
      {expanded && (
        <div className="slopr-activity-trace-body">
          {events.map((event) => (
            <ActivityTraceRow key={event.id} event={event} />
          ))}
        </div>
      )}
    </div>
  );
}

function ActivityTraceRow({ event }: { event: ActivityTraceEvent }) {
  if (event.type === "reasoning") {
    const iconClass =
      event.status === "done"
        ? "slopr-activity-trace-icon slopr-activity-trace-icon-reasoning slopr-activity-trace-icon-reasoning-complete"
        : "slopr-activity-trace-icon slopr-activity-trace-icon-reasoning";
    return (
      <div className="slopr-activity-trace-row">
        <span className={iconClass} aria-hidden="true" />
        <div className="slopr-activity-reasoning">
          <Markdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>
            {event.content.trim()}
          </Markdown>
        </div>
      </div>
    );
  }
  const status = activityToolStatusMeta(event);
  return (
    <div className="slopr-activity-trace-row">
      <span className="slopr-activity-trace-icon" aria-hidden="true">
        {event.summary.kind === "search" ? <GlobeTraceIcon /> : <FetchTraceIcon />}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-3">
          <span className="slopr-activity-tool-title">{event.summary.title}</span>
          <span className={`slopr-activity-status-pill shrink-0 ${status.className}`}>{status.label}</span>
        </div>
        <div className="slopr-activity-tool-detail">{event.summary.detail}</div>
        {event.preview?.kind === "searchResults" && event.preview.results.length > 0 && (
          <>
            <div className="slopr-activity-result-count">
              {event.preview.resultCount} {event.preview.resultCount === 1 ? "result" : "results"}
            </div>
            <div className="slopr-activity-result-list">
              {event.preview.results.map((result, index) => (
                <SearchResultRow key={`${result.url ?? result.title}-${index}`} result={result} />
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function SearchResultRow({
  result,
}: {
  result: { title: string; url?: string; domain?: string; snippet?: string };
}) {
  const favicon = result.url === undefined ? undefined : faviconURL(result.url);
  const href = result.url === undefined ? undefined : externalHTTPURL(result.url);
  const title = <div className="slopr-activity-result-title">{result.title}</div>;
  return (
    <div className="slopr-activity-result-row">
      {favicon !== undefined ? (
        <img alt="" className="slopr-activity-favicon" src={favicon} />
      ) : (
        <span className="slopr-activity-favicon" aria-hidden="true">
          {faviconInitial(result.domain ?? result.title)}
        </span>
      )}
      <div className="min-w-0">
        {href === undefined ? (
          title
        ) : (
          <a className="slopr-activity-result-link" href={href} target="_blank" rel="noreferrer">
            {title}
          </a>
        )}
      </div>
      {result.domain !== undefined && <div className="slopr-activity-result-domain">{result.domain}</div>}
    </div>
  );
}

function externalHTTPURL(value: string): string | undefined {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:" ? url.toString() : undefined;
  } catch {
    return undefined;
  }
}

function activityToolStatusMeta(event: ActivityTraceToolEvent): { label: string; className: string } {
  if (event.status === "failed") return { label: "Failed", className: "slopr-activity-status-failed" };
  if (event.status === "running") return { label: "Running", className: "slopr-activity-status-neutral" };
  return { label: "Done", className: "slopr-activity-status-neutral" };
}

function GlobeTraceIcon() {
  return (
    <svg className="slopr-activity-globe-icon" viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18" />
      <path d="M12 3c2.25 2.45 3.35 5.45 3.35 9s-1.1 6.55-3.35 9" />
      <path d="M12 3c-2.25 2.45-3.35 5.45-3.35 9s1.1 6.55 3.35 9" />
    </svg>
  );
}

function FetchTraceIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="M7 17 17 7" />
      <path d="M9 7h8v8" />
    </svg>
  );
}

function faviconInitial(value: string): string {
  return value.trim().charAt(0).toUpperCase() || "*";
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
  isSending,
  placeholder,
  onDraftChange,
  onSend,
  onStop,
}: {
  variant: "start" | "chat";
  draft: string;
  isSending: boolean;
  placeholder: string;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
}) {
  const height = variant === "start" ? "h-[122px]" : "h-[102px]";
  const sendIconClass = variant === "chat" ? "h-4 w-4 -translate-y-px" : "h-4 w-4";
  const padX = "px-6";
  const canSend = !isSending && draft.trim() !== "";
  const actionButtonClass = isSending
    ? "bg-[#3a3a37] hover:bg-[#4b4a46]"
    : "bg-accent hover:bg-accent-strong disabled:bg-accent";
  return (
    <form
      className={`slopr-composer ${height} relative rounded-[20px] border border-[#4b4a46] bg-[#2a2a28] shadow-[0_14px_24px_rgba(0,0,0,0.22)]`}
      onSubmit={(event) => {
        event.preventDefault();
        if (isSending) {
          onStop();
          return;
        }
        onSend();
      }}
    >
      <textarea
        className={`slopr-composer-text h-full w-full resize-none overflow-hidden bg-transparent ${padX} pb-14 pt-5 text-[#f3f0e8] outline-none placeholder:text-[#aaa79e]`}
        placeholder={placeholder}
        value={draft}
        onChange={(event) => onDraftChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.shiftKey) {
            event.preventDefault();
            if (!isSending) onSend();
          }
        }}
      />
      <div className={`absolute inset-x-0 bottom-0 flex h-11 items-center justify-between ${padX} text-[#d8d4ca]`}>
        <button className="text-2xl leading-none" type="button" aria-label="Add attachment">
          +
        </button>
        <div className="slopr-meta-text flex items-center text-[#d8d4ca]">
          <button
            className={`slopr-composer-send grid h-7 w-7 place-items-center rounded-md text-[#eeeae2] transition-colors disabled:cursor-not-allowed disabled:opacity-45 ${actionButtonClass}`}
            disabled={!isSending && !canSend}
            type="submit"
            aria-label={isSending ? "Stop response" : "Send message"}
          >
            {isSending ? (
              <svg className={sendIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="currentColor">
                <rect x="5.5" y="5.5" width="13" height="13" rx="2" />
              </svg>
            ) : (
              <svg className={sendIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="none">
                <path d="M12 19V5M6.5 10.5 12 5l5.5 5.5" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            )}
          </button>
        </div>
      </div>
    </form>
  );
}

function PromptChip({ icon, label }: { icon: string; label: string }) {
  return (
    <button className="slopr-meta-text flex h-8 items-center gap-1.5 rounded-lg bg-[#3a3a37] px-3 text-[#eeeae2]" type="button">
      <span className="text-[#aaa79e]">{icon}</span>
      {label}
    </button>
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
      <div className="slopr-user-message group ml-auto w-fit max-w-full md:max-w-[38.25rem]">
        <div className="slopr-message-text slopr-user-message-text rounded-xl bg-[#111110] px-4 py-3 text-[#f3f0e8]">
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
    <div className="slopr-codeblock">
      <button
        type="button"
        className="slopr-codeblock-copy"
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
    <div className="slopr-message-text slopr-markdown text-[#f3f0e8]">
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
      <div className="slopr-assistant-message group w-full space-y-3">
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
      <div className="slopr-assistant-message group w-full space-y-3">
        <ProseMarkdown>{before}</ProseMarkdown>
        <PendingDownloadResponseBubble label={label} receivedBytes={receivedBytes} />
      </div>
    );
  }

  return (
    <div className="slopr-assistant-message group w-full">
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
          <div className="slopr-message-text truncate">{label} response</div>
          <div className="slopr-meta-text text-[#aaa79e]">{progressText}</div>
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
          <div className="slopr-message-text truncate">{artifact.label} response</div>
          <div className="slopr-meta-text text-[#aaa79e]">Ready to download</div>
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

export function buildImageStats(artifact: Artifact): string | null {
  const segments: string[] = [];
  if (artifact.model) segments.push(artifact.model);
  if (artifact.width && artifact.height) segments.push(`${artifact.width}×${artifact.height}`);
  if (artifact.durationMs && artifact.durationMs > 0) segments.push(formatDuration(artifact.durationMs));
  return segments.length > 0 ? segments.join(" · ") : null;
}

export function GeneratedArtifactCard({ artifact }: { artifact: Artifact }) {
  const [error, setError] = useState("");
  const [previewUrl, setPreviewUrl] = useState("");
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const isImage = artifact.mimeType.startsWith("image/");
  const imageStats = isImage ? buildImageStats(artifact) : null;

  useEffect(() => {
    if (!isImage) {
      setPreviewUrl("");
      return;
    }
    let cancelled = false;
    let objectUrl = "";
    setError("");
    setPreviewUrl("");
    void downloadArtifact(artifact.downloadUrl)
      .then((blob) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setPreviewUrl(objectUrl);
      })
      .catch(() => {
        if (!cancelled) setError("Preview failed");
      });
    return () => {
      cancelled = true;
      if (objectUrl !== "") URL.revokeObjectURL(objectUrl);
    };
  }, [artifact.downloadUrl, isImage]);

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

  function handleOpenPreview() {
    if (previewUrl === "") return;
    setError("");
    setLightboxOpen(true);
  }

  useEffect(() => {
    if (!lightboxOpen) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setLightboxOpen(false);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [lightboxOpen]);

  return (
    <div className="max-w-[28rem] overflow-hidden rounded-lg border border-[#3e3d39] bg-[#282826] text-[#f3f0e8]">
      {isImage &&
        // Reserve the image's vertical space up-front so the card never collapses while the
        // blob loads asynchronously (or when it remounts on stream → committed). A collapse
        // would shrink scrollHeight and make the browser clamp scrollTop upward = unwanted
        // upward jump. With known dimensions we reserve the exact box via aspect-ratio;
        // otherwise we fall back to a min-height floor that bounds the collapse.
        (artifact.width && artifact.height ? (
          <button
            className="relative block max-h-[28rem] w-full cursor-zoom-in overflow-hidden bg-[#1f1f1d]"
            onClick={handleOpenPreview}
            type="button"
            title={`Preview ${artifact.displayFilename}`}
            aria-label={`Preview ${artifact.displayFilename}`}
            style={{ aspectRatio: `${artifact.width} / ${artifact.height}` }}
          >
            {previewUrl !== "" && (
              <img
                className="absolute inset-0 h-full w-full object-contain"
                src={previewUrl}
                alt={artifact.displayFilename}
                loading="lazy"
              />
            )}
          </button>
        ) : (
          <button
            className="block min-h-[16rem] w-full cursor-zoom-in bg-[#1f1f1d]"
            onClick={handleOpenPreview}
            type="button"
            title={`Preview ${artifact.displayFilename}`}
            aria-label={`Preview ${artifact.displayFilename}`}
          >
            {previewUrl !== "" && (
              <img
                className="block max-h-[28rem] w-full object-contain"
                src={previewUrl}
                alt={artifact.displayFilename}
                loading="lazy"
              />
            )}
          </button>
        ))}
      <div className="flex items-center gap-3 px-4 py-3">
        {!isImage && (
          <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
            <FileIcon />
          </div>
        )}
        <div className="min-w-0 flex-1">
          <div className="slopr-message-text truncate">{artifact.displayFilename}</div>
          <div className="slopr-meta-text text-[#aaa79e]">
            {artifact.mimeType} · {formatFileSize(artifact.sizeBytes)}
          </div>
          {imageStats !== null && (
            <div className="font-mono text-xs text-[#88857d]">{imageStats}</div>
          )}
          {error !== "" && <div className="slopr-meta-text text-[#d36f67]">{error}</div>}
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
      {lightboxOpen && previewUrl !== "" && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-6"
          onClick={() => setLightboxOpen(false)}
          role="dialog"
          aria-modal="true"
          aria-label={`Preview ${artifact.displayFilename}`}
        >
          <button
            className="absolute right-4 top-4 grid h-9 w-9 place-items-center rounded-md bg-black/40 text-[#f3f0e8] transition-colors hover:bg-black/60"
            onClick={() => setLightboxOpen(false)}
            type="button"
            title="Close preview"
            aria-label="Close preview"
          >
            <CloseIcon />
          </button>
          <img
            className="max-h-full max-w-full object-contain"
            src={previewUrl}
            alt={artifact.displayFilename}
            onClick={(event) => event.stopPropagation()}
          />
        </div>
      )}
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
  anchor.download = `slopr-response.${artifact.extension}`;
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
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M5.8 8h7.4c1 0 1.8.8 1.8 1.8v7.4c0 1-.8 1.8-1.8 1.8H5.8c-1 0-1.8-.8-1.8-1.8V9.8C4 8.8 4.8 8 5.8 8Z"
        stroke="currentColor"
        strokeWidth="1.5"
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
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
      <path d="M15 9.2a4 4 0 0 1 0 5.6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <path d="M17.6 6.6a7.5 7.5 0 0 1 0 10.8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

function CheckIcon({ className = "h-[1.33rem] w-[1.33rem]" }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="m5 12.5 4.2 4.2L19 7"
        stroke="currentColor"
        strokeWidth="1.5"
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

function CloseIcon() {
  return (
    <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M6 6 18 18M18 6 6 18"
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
        strokeWidth="1.5"
        strokeLinecap="round"
      />
      <path
        d="M18.5 5.5v3.7h-3.7"
        stroke="currentColor"
        strokeWidth="1.5"
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
    <div className="slopr-meta-text mt-3 max-w-3xl rounded-lg border border-accent bg-[#282826] px-4 py-3 text-accent">
      {children}
    </div>
  );
}
