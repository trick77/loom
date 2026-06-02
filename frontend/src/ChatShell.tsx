import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  AuthExpiredError,
  createProject,
  createThread,
  getMcpStatus,
  getThread,
  listProjects,
  listThreads,
  setThreadStarred,
  streamMessage,
  type McpStatusEvent,
  type Message,
  type Project,
  type Thread,
  type ToolCallEvent,
  type ToolResultEvent,
  type User,
} from "./api";
import logoImage from "./assets/logo.png";
import { MessageMetrics, MetricsProvider } from "./MessageMetrics";

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
  const [streamingText, setStreamingText] = useState("");
  const [streamingReasoning, setStreamingReasoning] = useState("");
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
      clearToolEvents();
      setSendError("");
      return;
    }
    if (activeThreadIDRef.current === route.threadID) return;
    let active = true;
    streamAbortRef.current?.abort();
    getThread(route.threadID)
      .then((response) => {
        if (!active) return;
        setActiveThread(response.thread);
        activeThreadIDRef.current = response.thread.id;
        setMessages(response.messages);
        setStreamingText("");
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

  async function handleSetActiveThreadStarred(starred: boolean) {
    if (activeThread === null || isUpdatingStar) return;
    const threadID = activeThread.id;
    setIsUpdatingStar(true);
    try {
      const updatedThread = await setThreadStarred(threadID, starred);
      if (activeThreadIDRef.current === updatedThread.id) {
        setActiveThread(updatedThread);
      }
      setThreads((current) =>
        current.map((thread) => (thread.id === updatedThread.id ? updatedThread : thread)),
      );
      setSendError("");
    } catch (error) {
      handleActionError(error, "Thread failed to update.", setSendError);
    } finally {
      setIsUpdatingStar(false);
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
        onAssistantMessage: (message) => {
          if (!isCurrentThread()) return;
          const completedToolEvents = toolEventsRef.current;
          setMessages((current) => [
            ...current,
            completedToolEvents.length > 0 ? { ...message, toolEvents: completedToolEvents } : message,
          ]);
          setStreamingText("");
          setStreamingReasoning("");
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
      clearToolEvents();
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
    <div className="grid h-screen grid-cols-[282px_1fr] bg-bg font-sans text-ink">
      <aside className="spark-sidebar-text flex min-h-0 flex-col border-r border-[#343432] bg-panel text-[#c7c5bd]">
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
            onSelect={selectThread}
          />
          <SidebarSection
            title="Recents"
            threads={threads}
            activeThreadID={route.view === "chat" ? route.threadID : null}
            onSelect={selectThread}
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
            <div className="space-y-1">
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
          {mcpStatus !== null && mcpStatus.configured > 0 && <McpStatusIndicator status={mcpStatus} />}
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
            toolEvents={toolEvents}
            sendError={sendError}
            isSending={isSending}
            isUpdatingStar={isUpdatingStar}
            onDraftChange={setDraft}
            onSend={handleSend}
            onRetry={handleRetry}
            onStarChange={handleSetActiveThreadStarred}
          />
        )}
      </main>
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
  const className = "h-[18px] w-[18px] shrink-0 text-[#f0eee7]";
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

function McpStatusIndicator({ status }: { status: McpStatusEvent }) {
  const allActive = status.active === status.configured;
  const ringClass = allActive ? "border-success" : "border-danger";
  const dotClass = allActive ? "bg-success" : "bg-danger";
  return (
    <div
      className="spark-meta-text mt-2 flex items-center gap-1.5 text-muted"
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
  onSelect,
}: {
  title: string;
  threads: Thread[];
  activeThreadID: string | null;
  onSelect(threadID: string): void;
}) {
  return (
    <section className="mt-5">
      <div className="spark-meta-text mb-2 px-1.5 text-[#97958c]">{title}</div>
      <div className="space-y-1">
        {threads.map((thread) => (
          <button
            key={thread.id}
            className={`block h-7 w-full truncate rounded-md px-1.5 text-left transition-colors hover:bg-[#2a2a28] ${
              activeThreadID === thread.id ? "bg-[#10100f] text-white" : ""
            }`}
            onClick={() => onSelect(thread.id)}
            type="button"
          >
            {thread.title}
          </button>
        ))}
      </div>
    </section>
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
  toolEvents,
  sendError,
  isSending,
  isUpdatingStar,
  onDraftChange,
  onSend,
  onRetry,
  onStarChange,
}: {
  thread: Thread | null;
  messages: MessageWithToolActivity[];
  draft: string;
  streamingText: string;
  streamingReasoning: string;
  toolEvents: ToolActivity[];
  sendError: string;
  isSending: boolean;
  isUpdatingStar: boolean;
  onDraftChange(value: string): void;
  onSend(): void;
  onRetry(content: string): void;
  onStarChange(starred: boolean): void;
}) {
  const transcriptRef = useRef<HTMLDivElement | null>(null);
  const shouldStickToBottomRef = useRef(true);
  const scrollFrameRef = useRef<number | null>(null);
  const [showJumpToBottom, setShowJumpToBottom] = useState(false);
  const showThinkingIndicator = isSending && streamingText === "" && streamingReasoning === "" && toolEvents.length === 0 && sendError === "";

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
  }, [messages.length, refreshScrollState, scrollToLatest, sendError, showThinkingIndicator, streamingReasoning, streamingText, toolEvents.length]);

  useEffect(() => {
    return () => {
      if (scrollFrameRef.current !== null) window.cancelAnimationFrame(scrollFrameRef.current);
    };
  }, []);

  return (
    <section className="flex h-screen min-h-0 flex-col">
      <header className="spark-control-text flex h-9 shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 text-[#d5d2c9]">
        <h1 className="min-w-0 max-w-[28ch] truncate font-sans font-normal sm:max-w-[48ch]">
          {thread?.title ?? "New chat"}
          <span className="ml-2 text-[#88857d]" aria-hidden="true">
            ⌄
          </span>
        </h1>
        {thread !== null && (
          <button
            className="spark-meta-text rounded-md px-2 py-1 text-[#aaa79e] transition-colors hover:bg-[#2a2a28] hover:text-white disabled:opacity-50"
            disabled={isUpdatingStar}
            onClick={() => onStarChange(!thread.starred)}
            type="button"
          >
            {thread.starred ? "Unstar chat" : "Star chat"}
          </button>
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
          <MetricsProvider>
            <div className="spark-chat-rail mx-auto w-full max-w-[720px] flex-1 space-y-6">
              {messages.map((message, index) => (
                <div key={message.id} className="space-y-6">
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
              {toolEvents.length > 0 && <ToolActivityPanel events={toolEvents} />}
              {showThinkingIndicator && <ThinkingIndicator />}
              {streamingReasoning !== "" && <ThinkingPanel content={streamingReasoning} complete={streamingText !== ""} />}
              {streamingText !== "" && <AssistantText>{streamingText}</AssistantText>}
              {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
            </div>
          </MetricsProvider>
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

function ThinkingIndicator() {
  return (
    <div className="spark-thinking-indicator" role="status" aria-label="Spark is thinking">
      <span className="spark-thinking-dot" aria-hidden="true" />
      <span className="spark-thinking-dot" aria-hidden="true" />
      <span className="spark-thinking-dot" aria-hidden="true" />
    </div>
  );
}

function ThinkingPanel({ content, complete }: { content: string; complete: boolean }) {
  const [expanded, setExpanded] = useState(false);
  const trimmed = content.trim();
  if (trimmed === "") return null;
  return (
    <div className="spark-thinking-panel">
      <button
        aria-expanded={expanded}
        aria-label={expanded ? "Hide thinking" : "Show thinking"}
        className="spark-thinking-panel-toggle"
        type="button"
        onClick={() => setExpanded((current) => !current)}
      >
        <span className="spark-thinking-panel-label">
          <span className={complete ? "spark-thinking-status-complete" : "spark-thinking-status-active"} aria-hidden="true" />
          <span>{complete ? "Thinking" : "Thinking..."}</span>
        </span>
        <span aria-hidden="true" className={expanded ? "spark-thinking-chevron-expanded" : "spark-thinking-chevron"}>
          ^
        </span>
      </button>
      {expanded && (
        <div className="spark-thinking-panel-body">
          <Markdown remarkPlugins={[remarkGfm]}>{trimmed}</Markdown>
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
  // Chat composer box is 34px wider than the 720px message rail; add 17px of
  // horizontal padding per side so the typed text stays within a 720px field.
  const padX = variant === "chat" ? "px-[41px]" : "px-6";
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

function ToolActivityPanel({ events }: { events: ToolActivity[] }) {
  return (
    <div className="spark-meta-text max-w-3xl rounded-lg border border-[#3e3d39] bg-[#282826] px-4 py-3 text-[#aaa79e]">
      <div className="font-medium text-[#f3f0e8]">Tools</div>
      <div className="mt-2 space-y-1">
        {events.map((event) => (
          <div key={event.id} className="flex items-center justify-between gap-3">
            <span className="min-w-0 truncate">{event.name}</span>
            <span className="shrink-0">{event.status === "done" ? "Done" : "Running"}</span>
          </div>
        ))}
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
        />
      </div>
    );
  }
  return (
    <div className="max-w-[46rem] space-y-3">
      {message.reasoningContent && <ThinkingPanel content={message.reasoningContent} complete={true} />}
      <AssistantText metricsMessage={message} onRetry={retryContent === null ? undefined : () => onRetry(retryContent)}>
        {message.content}
      </AssistantText>
    </div>
  );
}

function ProseMarkdown({ children }: { children: string }) {
  return (
    <div className="spark-message-text spark-markdown text-[#f3f0e8]">
      <Markdown
        remarkPlugins={[remarkGfm]}
        components={{
          a({ children, ...props }) {
            return (
              <a {...props} target="_blank" rel="noreferrer">
                {children}
              </a>
            );
          },
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
        />
        {metricsMessage && <MessageMetrics message={metricsMessage} />}
      </div>
    );
  }

  const pendingArtifact = pendingFencedArtifact(children);
  if (pendingArtifact !== null) {
    const { before, label } = pendingArtifact;
    if (before === "") {
      return <PendingDownloadResponseBubble label={label} />;
    }
    return (
      <div className="spark-assistant-message group w-full space-y-3">
        <ProseMarkdown>{before}</ProseMarkdown>
        <PendingDownloadResponseBubble label={label} />
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
      />
      {metricsMessage && <MessageMetrics message={metricsMessage} />}
    </div>
  );
}

function MessageActions({
  copyLabel,
  copyText,
  retryLabel,
  onRetry,
}: {
  copyLabel: string;
  copyText: string;
  retryLabel: string;
  onRetry?: () => void;
}) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    await copyResponse(copyText);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1200);
  }

  return (
    <div className="mt-3 flex items-center gap-2 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
      <button
        className="grid h-5 w-5 place-items-center text-[#c7c5bd] transition-colors hover:text-[#f3f0e8]"
        onClick={handleCopy}
        type="button"
        title="Copy"
        aria-label={copyLabel}
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </button>
      {onRetry !== undefined && (
        <button
          className="grid h-5 w-5 place-items-center text-[#c7c5bd] transition-colors hover:text-[#f3f0e8]"
          onClick={onRetry}
          type="button"
          title="Retry"
          aria-label={retryLabel}
        >
          <RetryIcon />
        </button>
      )}
    </div>
  );
}

function PendingDownloadResponseBubble({ label }: { label: string }) {
  return (
    <div className="max-w-[26rem] rounded-lg border border-[#3e3d39] bg-[#282826] px-4 py-3 text-[#f3f0e8]">
      <div className="flex items-center gap-3">
        <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
          <FileIcon />
        </div>
        <div className="min-w-0 flex-1">
          <div className="spark-message-text truncate">{label} response</div>
          <div className="spark-meta-text text-[#aaa79e]">Receiving file...</div>
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
          onClick={() => downloadArtifact(artifact)}
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

async function copyResponse(content: string) {
  await navigator.clipboard?.writeText(content);
}

function downloadArtifact(artifact: DownloadableResponse) {
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
  };
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

function CopyIcon() {
  return (
    <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" aria-hidden="true">
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

function CheckIcon() {
  return (
    <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" aria-hidden="true">
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
    <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" aria-hidden="true">
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
