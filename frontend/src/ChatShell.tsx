import { useEffect, useMemo, useRef, useState } from "react";
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

type ChatShellProps = {
  user: User;
  adminPanel: React.ReactNode;
  showAdmin: boolean;
  onAdmin(): void;
  onChat(): void;
  onLogout(): void;
  onSessionExpired(): void;
};

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
  const [isCreatingThread, setIsCreatingThread] = useState(false);
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

  const starredThreads = useMemo(() => threads.filter((thread) => thread.starred), [threads]);

  async function selectThread(threadID: string) {
    onChat();
    streamAbortRef.current?.abort();
    const response = await getThread(threadID);
    setActiveThread(response.thread);
    activeThreadIDRef.current = response.thread.id;
    setMessages(response.messages);
    setStreamingText("");
    setToolEvents([]);
    setSendError("");
  }

  async function handleNewChat() {
    if (isCreatingThread) return;
    onChat();
    setIsCreatingThread(true);
    try {
      const thread = await createThread();
      setThreads((current) => [thread, ...current.filter((item) => item.id !== thread.id)]);
      await selectThread(thread.id);
    } catch (error) {
      handleActionError(error, "Message failed to send.", setSendError);
    } finally {
      setIsCreatingThread(false);
    }
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
    try {
      let targetThread = activeThread;
      if (targetThread === null) {
        const createdThread = await createThread();
        setThreads((current) => [
          createdThread,
          ...current.filter((item) => item.id !== createdThread.id),
        ]);
        setActiveThread(createdThread);
        activeThreadIDRef.current = createdThread.id;
        setMessages([]);
        targetThread = createdThread;
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
          if (isCurrentThread()) setActiveThread(updatedThread);
          setThreads((current) =>
            current.map((item) => (item.id === updatedThread.id ? updatedThread : item)),
          );
        },
        onMcpStatus: (event) => setMcpStatus(event),
      }, abortController.signal);
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
    <div className="grid h-screen grid-cols-[240px_1fr_300px] font-sans text-ink">
      <aside className="flex min-h-0 flex-col gap-3 border-r border-border bg-panel p-3">
        <div className="flex items-center px-1">
          <div className="font-serif text-xl font-medium tracking-tight">Spark</div>
        </div>
        <button
          className="rounded-spark bg-accent px-3 py-2 text-left text-sm font-medium text-white transition-colors hover:bg-accent-strong"
          disabled={isCreatingThread}
          onClick={handleNewChat}
        >
          + New chat
        </button>
        {loadError !== "" && <div className="rounded-spark border border-accent p-2 text-xs text-accent">{loadError}</div>}
        <SidebarSection title="Starred" threads={starredThreads} onSelect={selectThread} />
        <SidebarSection title="Recents" threads={threads} onSelect={selectThread} />
        <section>
          <div className="mb-2 flex items-center justify-between gap-2">
            <div className="text-xs font-semibold uppercase text-muted">Projects</div>
            <button
              className="text-xs font-medium text-muted transition-colors hover:text-ink"
              onClick={() => setIsProjectFormOpen(true)}
              type="button"
            >
              New project
            </button>
          </div>
          {isProjectFormOpen && (
            <form
              className="mb-2 space-y-2"
              onSubmit={(event) => {
                event.preventDefault();
                handleCreateProject();
              }}
            >
              <input
                autoFocus
                className="w-full rounded-spark border border-border bg-bg px-2 py-1.5 text-sm text-ink outline-none placeholder:text-muted focus:border-accent"
                placeholder="Project name"
                value={projectName}
                onChange={(event) => setProjectName(event.target.value)}
              />
              <div className="flex gap-2">
                <button
                  className="rounded-spark bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent-strong disabled:opacity-50"
                  disabled={projectName.trim() === "" || isCreatingProject}
                  type="submit"
                >
                  Create
                </button>
                <button
                  className="px-2 py-1.5 text-xs text-muted transition-colors hover:text-ink"
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
              <div key={project.id} className="rounded-spark px-2 py-1 text-sm">
                {project.name}
              </div>
            ))}
          </div>
        </section>
        {user.role === "admin" && (
          <button
            className="rounded-spark bg-active px-3 py-2 text-left text-sm transition-colors hover:bg-border"
            onClick={onAdmin}
          >
            Admin
          </button>
        )}
        <div className="mt-auto border-t border-border pt-3">
          <div className="text-sm font-medium">{user.displayName || user.username}</div>
          <div className="text-xs text-muted">{user.role}</div>
          {mcpStatus !== null && mcpStatus.configured > 0 && <McpStatusIndicator status={mcpStatus} />}
          <button className="mt-2 text-sm text-muted" onClick={onLogout}>
            Logout
          </button>
        </div>
      </aside>
      <main className="flex min-h-0 flex-col bg-bg">
        {showAdmin ? (
          adminPanel
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
      <aside className="border-l border-border bg-panel p-3 text-sm text-muted">
        <div className="font-medium text-ink">Sources</div>
        <p className="mt-2">Sources will appear with document and web answers.</p>
      </aside>
    </div>
  );
}

type ToolActivity = {
  id: string;
  name: string;
  status: "running" | "done";
  content?: string;
};

function upsertToolCall(current: ToolActivity[], event: ToolCallEvent): ToolActivity[] {
  const next = current.filter((item) => item.id !== event.id);
  return [...next, { id: event.id, name: event.name, status: "running" }];
}

function upsertToolResult(current: ToolActivity[], event: ToolResultEvent): ToolActivity[] {
  return current.map((item) =>
    item.id === event.id ? { ...item, status: "done", content: event.content } : item,
  );
}

function McpStatusIndicator({ status }: { status: McpStatusEvent }) {
  const allActive = status.active === status.configured;
  const ringClass = allActive ? "border-success" : "border-danger";
  const dotClass = allActive ? "bg-success" : "bg-danger";
  return (
    <div
      className="mt-2 flex items-center gap-1.5 text-xs text-muted"
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
  onSelect,
}: {
  title: string;
  threads: Thread[];
  onSelect(threadID: string): void;
}) {
  return (
    <section>
      <div className="mb-2 text-xs font-semibold uppercase text-muted">{title}</div>
      <div className="space-y-1">
        {threads.map((thread) => (
          <button
            key={thread.id}
            className="block w-full truncate rounded-spark px-2 py-1 text-left text-sm transition-colors hover:bg-active"
            onClick={() => onSelect(thread.id)}
          >
            {thread.title}
          </button>
        ))}
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
    <>
      <header className="flex items-center justify-between gap-4 border-b border-border px-6 py-4">
        <h1 className="min-w-0 truncate font-serif text-2xl font-light tracking-tight">
          {thread?.title ?? "New chat"}
        </h1>
        {thread !== null && (
          <button
            className="shrink-0 rounded-spark border border-border px-3 py-1.5 text-sm text-muted transition-colors hover:text-ink disabled:opacity-50"
            disabled={isUpdatingStar}
            onClick={() => onStarChange(!thread.starred)}
            type="button"
          >
            {thread.starred ? "Unstar chat" : "Star chat"}
          </button>
        )}
      </header>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-6 py-5">
        {messages.map((message) => (
          <MessageBubble key={message.id} message={message} />
        ))}
        {toolEvents.length > 0 && <ToolActivityPanel events={toolEvents} />}
        {streamingText !== "" && (
          <div className="max-w-3xl rounded-spark border border-border bg-panel px-4 py-3 text-sm">
            {streamingText}
          </div>
        )}
        {sendError !== "" && (
          <div className="max-w-3xl rounded-spark border border-accent bg-panel px-4 py-3 text-sm text-accent">
            {sendError}
          </div>
        )}
      </div>
      <form
        className="border-t border-border p-4"
        onSubmit={(event) => {
          event.preventDefault();
          onSend();
        }}
      >
        <div className="flex gap-2">
          <input
            className="min-w-0 flex-1 rounded-spark border border-border bg-panel px-3 py-2 text-sm text-ink outline-none placeholder:text-muted focus:border-accent"
            placeholder="Message"
            value={draft}
            onChange={(event) => onDraftChange(event.target.value)}
          />
          <button
            className="rounded-spark bg-accent px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-accent-strong disabled:opacity-50"
            disabled={isSending || draft.trim() === ""}
            type="submit"
          >
            Send
          </button>
        </div>
      </form>
    </>
  );
}

function ToolActivityPanel({ events }: { events: ToolActivity[] }) {
  return (
    <div className="max-w-3xl rounded-spark border border-border bg-panel px-4 py-3 text-sm text-muted">
      <div className="font-medium text-ink">Tools</div>
      <div className="mt-2 space-y-1">
        {events.map((event) => (
          <div key={event.id} className="flex items-center justify-between gap-3">
            <span className="min-w-0 truncate">{event.name}</span>
            <span className="shrink-0 text-xs">{event.status === "done" ? "Done" : "Running"}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function MessageBubble({ message }: { message: Message }) {
  const isUser = message.role === "user";
  return (
    <div
      className={`max-w-3xl rounded-spark border border-border px-4 py-3 text-sm ${
        isUser ? "ml-auto bg-active" : "bg-panel"
      }`}
    >
      {message.content}
    </div>
  );
}
