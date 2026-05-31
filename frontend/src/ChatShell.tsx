import { useEffect, useMemo, useState } from "react";
import {
  createThread,
  getThread,
  listProjects,
  listThreads,
  streamMessage,
  type Message,
  type Project,
  type Thread,
  type User,
} from "./api";

type ChatShellProps = {
  user: User;
  adminPanel: React.ReactNode;
  showAdmin: boolean;
  onAdmin(): void;
  onChat(): void;
  onLogout(): void;
};

export function ChatShell({ user, adminPanel, showAdmin, onAdmin, onChat, onLogout }: ChatShellProps) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [threads, setThreads] = useState<Thread[]>([]);
  const [activeThread, setActiveThread] = useState<Thread | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [streamingText, setStreamingText] = useState("");
  const [isSending, setIsSending] = useState(false);

  useEffect(() => {
    let active = true;
    Promise.all([listProjects(), listThreads({ limit: 30 })]).then(([nextProjects, nextThreads]) => {
      if (!active) return;
      setProjects(nextProjects);
      setThreads(nextThreads);
    });
    return () => {
      active = false;
    };
  }, []);

  const starredThreads = useMemo(() => threads.filter((thread) => thread.starred), [threads]);

  async function selectThread(threadID: string) {
    onChat();
    const response = await getThread(threadID);
    setActiveThread(response.thread);
    setMessages(response.messages);
    setStreamingText("");
  }

  async function handleNewChat() {
    onChat();
    const thread = await createThread();
    setThreads((current) => [thread, ...current.filter((item) => item.id !== thread.id)]);
    await selectThread(thread.id);
  }

  async function handleSend() {
    const content = draft.trim();
    if (content === "" || isSending) return;
    setDraft("");
    setIsSending(true);
    setStreamingText("");
    try {
      let targetThread = activeThread;
      if (targetThread === null) {
        const createdThread = await createThread();
        setThreads((current) => [
          createdThread,
          ...current.filter((item) => item.id !== createdThread.id),
        ]);
        setActiveThread(createdThread);
        setMessages([]);
        targetThread = createdThread;
      }
      await streamMessage(targetThread.id, content, {
        onUserMessage: (message) => setMessages((current) => [...current, message]),
        onDelta: (delta) => setStreamingText((current) => current + delta),
        onAssistantMessage: (message) => {
          setMessages((current) => [...current, message]);
          setStreamingText("");
        },
        onThread: (updatedThread) => {
          setActiveThread(updatedThread);
          setThreads((current) =>
            current.map((item) => (item.id === updatedThread.id ? updatedThread : item)),
          );
        },
      });
    } finally {
      setIsSending(false);
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
          onClick={handleNewChat}
        >
          + New chat
        </button>
        <SidebarSection title="Starred" threads={starredThreads} onSelect={selectThread} />
        <SidebarSection title="Recents" threads={threads} onSelect={selectThread} />
        <section>
          <div className="mb-2 text-xs font-semibold uppercase text-muted">Projects</div>
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
            isSending={isSending}
            onDraftChange={setDraft}
            onSend={handleSend}
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
  isSending,
  onDraftChange,
  onSend,
}: {
  thread: Thread | null;
  messages: Message[];
  draft: string;
  streamingText: string;
  isSending: boolean;
  onDraftChange(value: string): void;
  onSend(): void;
}) {
  return (
    <>
      <header className="border-b border-border px-6 py-4">
        <h1 className="font-serif text-2xl font-light tracking-tight">
          {thread?.title ?? "New chat"}
        </h1>
      </header>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-6 py-5">
        {messages.map((message) => (
          <MessageBubble key={message.id} message={message} />
        ))}
        {streamingText !== "" && (
          <div className="max-w-3xl rounded-spark border border-border bg-panel px-4 py-3 text-sm">
            {streamingText}
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
