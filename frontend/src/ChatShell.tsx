import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  AuthExpiredError,
  createProject,
  createThread,
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

type SidebarIconName = "chats" | "projects" | "artifacts" | "customize";

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
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [projectName, setProjectName] = useState("");
  const [isProjectFormOpen, setIsProjectFormOpen] = useState(false);
  const [isCreatingProject, setIsCreatingProject] = useState(false);
  const [streamingText, setStreamingText] = useState("");
  const [toolEvents, setToolEvents] = useState<ToolActivity[]>([]);
  const [mcpStatus, setMcpStatus] = useState<McpStatusEvent | null>(null);
  const [sendError, setSendError] = useState("");
  const [loadError, setLoadError] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [isUpdatingStar, setIsUpdatingStar] = useState(false);
  const activeThreadIDRef = useRef<string | null>(null);
  const streamAbortRef = useRef<AbortController | null>(null);

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
    if (route.view !== "chat") {
      activeThreadIDRef.current = null;
      setActiveThread(null);
      setMessages([]);
      setStreamingText("");
      setToolEvents([]);
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
        setToolEvents([]);
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
    setToolEvents([]);
    setSendError("");
    navigate({ view: "new" });
    setRoute({ view: "new" });
  }, [onChat]);

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
    setDraft("");
    setIsSending(true);
    setStreamingText("");
    setToolEvents([]);
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
        onToolCall: (event) => {
          if (!isCurrentThread()) return;
          setToolEvents((current) => upsertToolCall(current, event));
        },
        onToolResult: (event) => {
          if (!isCurrentThread()) return;
          setToolEvents((current) => upsertToolResult(current, event));
        },
        onAssistantMessage: (message) => {
          if (!isCurrentThread()) return;
          setMessages((current) => [...current, message]);
          setStreamingText("");
          setToolEvents([]);
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
      setToolEvents([]);
      setDraft(content);
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
      <aside className="flex min-h-0 flex-col border-r border-[#343432] bg-panel text-[13px] text-[#c7c5bd]">
        <div className="flex h-11 items-center justify-between px-3">
          <div className="font-serif text-[22px] font-medium text-[#f4f0e8]">Spark</div>
          <div className="flex items-center gap-3 text-[#aaa79e]" aria-hidden="true">
            <span className="text-sm">⌕</span>
            <span className="text-xs">▯</span>
          </div>
        </div>
        <nav className="min-h-0 flex-1 overflow-y-auto px-2 pb-4 pt-2">
          <button
            className={`flex h-8 w-full items-center gap-2.5 rounded-md px-2 text-left transition-colors hover:bg-[#2a2a28] ${
              route.view === "new" && !showAdmin ? "bg-[#111110] text-white" : ""
            }`}
            onClick={navigateToNew}
            type="button"
          >
            <span className="grid h-[22px] w-[22px] place-items-center rounded-full bg-[#30302e] text-[17px] leading-none text-[#d5d2c9]">
              +
            </span>
            <span>New chat</span>
          </button>
          <SidebarPrimaryItem label="Chats" icon="chats" />
          <SidebarPrimaryItem label="Projects" icon="projects" />
          <SidebarPrimaryItem label="Artifacts" icon="artifacts" />
          <SidebarPrimaryItem label="Customize" icon="customize" />
          {loadError !== "" && (
            <div className="mx-2 mt-3 rounded-md border border-accent px-2 py-2 text-[11px] text-accent">
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
          <section className="mt-4">
            <div className="mb-2 flex items-center justify-between px-2 text-[11px] text-[#8f8b82]">
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
                  className="w-full rounded-md border border-[#3b3b38] bg-[#20201f] px-2 py-1.5 text-xs text-ink outline-none placeholder:text-muted focus:border-[#69665f]"
                  placeholder="Project name"
                  value={projectName}
                  onChange={(event) => setProjectName(event.target.value)}
                />
                <div className="flex gap-2">
                  <button
                    className="rounded-md bg-[#393936] px-3 py-1.5 text-[11px] font-medium text-white disabled:opacity-50"
                    disabled={projectName.trim() === "" || isCreatingProject}
                    type="submit"
                  >
                    Create
                  </button>
                  <button
                    className="px-2 py-1.5 text-[11px] text-muted transition-colors hover:text-ink"
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
                <div key={project.id} className="truncate rounded-md px-2 py-1.5 text-[13px]">
                  {project.name}
                </div>
              ))}
            </div>
          </section>
          {user.role === "admin" && (
            <button
              className="mt-4 flex h-8 w-full items-center rounded-md px-2 text-left text-[13px] transition-colors hover:bg-[#2a2a28]"
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
              <div className="truncate text-xs text-[#f4f0e8]">{displayName}</div>
              <div className="truncate text-[10px] text-[#8f8b82]">{user.role}</div>
            </div>
            <button className="rounded-md px-2 py-1 text-[10px] text-[#aaa79e] hover:bg-[#2a2a28]" onClick={onLogout}>
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
            toolEvents={toolEvents}
            sendError={sendError}
            isSending={isSending}
            isUpdatingStar={isUpdatingStar}
            onDraftChange={setDraft}
            onSend={handleSend}
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
    <div className="mt-0.5 flex h-8 items-center gap-2.5 rounded-md px-2 text-[#c7c5bd]">
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
  if (name === "artifacts") {
    return (
      <svg className={className} viewBox="0 0 24 24" aria-hidden="true" fill="none">
        <path d="M12 4.5v5.2M7.5 12.3 12 9.7l4.5 2.6M7.5 17.5v-5.2M16.5 17.5v-5.2" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
        <path d="M12 2.8 9.7 4.1v2.6L12 8l2.3-1.3V4.1L12 2.8ZM7.5 10.2l-2.3 1.3v2.6l2.3 1.3 2.3-1.3v-2.6l-2.3-1.3ZM16.5 10.2l-2.3 1.3v2.6l2.3 1.3 2.3-1.3v-2.6l-2.3-1.3ZM7.5 16.2l-2.3 1.3v2.6l2.3 1.3 2.3-1.3v-2.6l-2.3-1.3ZM16.5 16.2l-2.3 1.3v2.6l2.3 1.3 2.3-1.3v-2.6l-2.3-1.3Z" stroke="currentColor" strokeWidth="1.5" strokeLinejoin="round" />
      </svg>
    );
  }
  return (
    <svg className={className} viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path d="M8.5 7V5.7c0-1 .7-1.7 1.8-1.7h3.4c1.1 0 1.8.7 1.8 1.7V7" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M5 7h14c1.2 0 2 .8 2 2v8.5c0 1.3-.8 2-2 2H5c-1.2 0-2-.7-2-2V9c0-1.2.8-2 2-2Z" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
      <path d="M3.5 12h17M10 12.2v1.2h4v-1.2" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function McpStatusIndicator({ status }: { status: McpStatusEvent }) {
  const allActive = status.active === status.configured;
  const ringClass = allActive ? "border-success" : "border-danger";
  const dotClass = allActive ? "bg-success" : "bg-danger";
  return (
    <div
      className="mt-2 flex items-center gap-1.5 text-[11px] text-muted"
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
    <section className="mt-4">
      <div className="mb-2 px-2 text-[11px] text-[#8f8b82]">{title}</div>
      <div className="space-y-1">
        {threads.map((thread) => (
          <button
            key={thread.id}
            className={`block h-8 w-full truncate rounded-md px-2 text-left text-[13px] transition-colors hover:bg-[#2a2a28] ${
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
      <h1 className="mb-8 flex items-center gap-4 font-serif text-[44px] font-light leading-none text-[#d8d4ca]">
        <img className="h-11 w-11 shrink-0" src={logoImage} alt="" aria-hidden="true" />
        {greetingForNow()}, {displayName}
      </h1>
      <div className="w-full max-w-[676px]">
        <Composer
          variant="start"
          draft={draft}
          disabled={isSending}
          placeholder="How can I help you today?"
          onDraftChange={onDraftChange}
          onSend={onSend}
        />
        {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
        <div className="mt-4 flex justify-center gap-2 text-xs text-[#e8e4da]">
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
  toolEvents,
  sendError,
  isSending,
  isUpdatingStar,
  onDraftChange,
  onSend,
  onStarChange,
}: {
  thread: Thread | null;
  messages: Message[];
  draft: string;
  streamingText: string;
  toolEvents: ToolActivity[];
  sendError: string;
  isSending: boolean;
  isUpdatingStar: boolean;
  onDraftChange(value: string): void;
  onSend(): void;
  onStarChange(starred: boolean): void;
}) {
  return (
    <section className="flex h-screen min-h-0 flex-col">
      <header className="flex h-9 shrink-0 items-center justify-between border-b border-[#252523] px-4 text-[13px] text-[#d5d2c9]">
        <h1 className="min-w-0 truncate font-sans text-[13px] font-normal">
          {thread?.title ?? "New chat"}
          <span className="ml-2 text-[#88857d]" aria-hidden="true">
            ⌄
          </span>
        </h1>
        {thread !== null && (
          <button
            className="rounded-md px-2 py-1 text-[11px] text-[#aaa79e] transition-colors hover:bg-[#2a2a28] hover:text-white disabled:opacity-50"
            disabled={isUpdatingStar}
            onClick={() => onStarChange(!thread.starred)}
            type="button"
          >
            {thread.starred ? "Unstar chat" : "Star chat"}
          </button>
        )}
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto px-8 py-10">
        <div className="mx-auto w-full max-w-[834px] space-y-5">
          {messages.map((message) => (
            <MessageBubble key={message.id} message={message} />
          ))}
          {toolEvents.length > 0 && <ToolActivityPanel events={toolEvents} />}
          {streamingText !== "" && <AssistantText>{streamingText}</AssistantText>}
          {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
        </div>
      </div>
      <div className="shrink-0 px-8 pb-5">
        <div className="mx-auto w-full max-w-[756px]">
          <Composer
            variant="chat"
            draft={draft}
            disabled={isSending}
            placeholder="Write a message..."
            onDraftChange={onDraftChange}
            onSend={onSend}
          />
          <div className="mt-2 text-center text-[11px] text-[#858178]">
            Spark can make mistakes. Please double-check responses.
          </div>
        </div>
      </div>
    </section>
  );
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
  const height = variant === "start" ? "h-[122px]" : "h-[104px]";
  return (
    <form
      className={`${height} rounded-[20px] border border-[#4b4a46] bg-[#2a2a28] shadow-[0_14px_24px_rgba(0,0,0,0.22)]`}
      onSubmit={(event) => {
        event.preventDefault();
        onSend();
      }}
    >
      <textarea
        className="h-[58px] w-full resize-none overflow-hidden bg-transparent px-6 pt-5 text-xs text-[#f3f0e8] outline-none placeholder:text-[#aaa79e]"
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
      <div className="flex h-11 items-center justify-between px-6 text-[#d8d4ca]">
        <button className="text-2xl leading-none" type="button" aria-label="Add attachment">
          +
        </button>
        <div className="flex items-center text-xs text-[#d8d4ca]">
          <button
            className="grid h-8 w-8 place-items-center rounded-full bg-[#d8d4ca] text-sm font-medium text-[#1f1f1d] transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-35"
            disabled={disabled || draft.trim() === ""}
            type="submit"
            aria-label="Send message"
          >
            ↑
          </button>
        </div>
      </div>
    </form>
  );
}

function PromptChip({ icon, label }: { icon: string; label: string }) {
  return (
    <button className="flex h-8 items-center gap-1.5 rounded-lg bg-[#3a3a37] px-3 text-xs text-[#eeeae2]" type="button">
      <span className="text-[#aaa79e]">{icon}</span>
      {label}
    </button>
  );
}

function ToolActivityPanel({ events }: { events: ToolActivity[] }) {
  return (
    <div className="max-w-3xl rounded-lg border border-[#3e3d39] bg-[#282826] px-4 py-3 text-xs text-[#aaa79e]">
      <div className="font-medium text-[#f3f0e8]">Tools</div>
      <div className="mt-2 space-y-1">
        {events.map((event) => (
          <div key={event.id} className="flex items-center justify-between gap-3">
            <span className="min-w-0 truncate">{event.name}</span>
            <span className="shrink-0 text-[11px]">{event.status === "done" ? "Done" : "Running"}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function MessageBubble({ message }: { message: Message }) {
  if (message.role === "user") {
    return (
      <div className="ml-auto max-w-[40rem] rounded-xl bg-[#111110] px-4 py-3 text-sm leading-relaxed text-[#f3f0e8]">
        {message.content}
      </div>
    );
  }
  return <AssistantText>{message.content}</AssistantText>;
}

function AssistantText({ children }: { children: React.ReactNode }) {
  return <div className="max-w-[46rem] text-sm leading-6 text-[#f3f0e8]">{children}</div>;
}

function ErrorText({ children }: { children: React.ReactNode }) {
  return (
    <div className="mt-3 max-w-3xl rounded-lg border border-accent bg-[#282826] px-4 py-3 text-xs text-accent">
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
