export type Role = "admin" | "user";

export type User = {
  id: string;
  username: string;
  email?: string;
  displayName?: string;
  role: Role;
  responseLanguage?: string;
};

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
  reasoningContent?: string;
  artifacts?: Artifact[];
  createdAt: string;
  promptTokens?: number;
  completionTokens?: number;
  totalTokens?: number;
  cachedTokens?: number;
  reasoningTokens?: number;
  durationMs?: number;
  model?: string;
  reasoningEffort?: string;
};

export type Artifact = {
  id: string;
  displayFilename: string;
  mimeType: string;
  sizeBytes: number;
  projectId?: string;
  downloadUrl: string;
  model?: string;
  provider?: string;
  width?: number;
  height?: number;
  durationMs?: number;
};

export type ToolCallEvent = {
  id: string;
  name: string;
  arguments: string;
};

export type ToolResultEvent = {
  id: string;
  name: string;
  content: string;
};

export type McpStatusEvent = {
  active: number;
  configured: number;
};

type ThreadResponse = {
  thread: Thread;
  messages: Message[];
};

type StreamHandlers = {
  onUserMessage(message: Message): void;
  onDelta(delta: string): void;
  onReasoningDelta?(delta: string): void;
  onAssistantMessage(message: Message): void;
  onThread(thread: Thread): void;
  onToolCall?(event: ToolCallEvent): void;
  onToolResult?(event: ToolResultEvent): void;
  onMcpStatus?(event: McpStatusEvent): void;
  onArtifact?(artifact: Artifact): void;
};

export class AuthExpiredError extends Error {
  constructor() {
    super("auth expired");
  }
}

export async function getMe(): Promise<User | null> {
  const response = await fetch("/api/me");
  if (response.status === 401) {
    return null;
  }
  if (!response.ok) {
    throw new Error("failed to load current user");
  }
  return response.json();
}

export async function listUsers(): Promise<User[]> {
  const response = await fetch("/api/admin/users");
  if (!response.ok) {
    throw new Error("failed to load users");
  }
  return response.json();
}

export async function logout(): Promise<string> {
  const response = await fetch("/api/auth/logout", { method: "POST" });
  if (!response.ok) {
    throw new Error("failed to log out");
  }
  const body = (await response.json()) as { redirectUrl?: string };
  return body.redirectUrl ?? "/";
}

export async function listProjects(): Promise<Project[]> {
  const response = await fetch("/api/projects");
  return expectJSON<Project[]>(response, "failed to load projects");
}

export async function getMcpStatus(): Promise<McpStatusEvent> {
  const response = await fetch("/api/mcp/status");
  return expectJSON<McpStatusEvent>(response, "failed to load MCP status");
}

export async function createProject(input: { name: string; description?: string }): Promise<Project> {
  const response = await fetch("/api/projects", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return expectJSON<Project>(response, "failed to create project");
}

export async function listThreads(params: {
  projectId?: string | null;
  starred?: boolean;
  archived?: boolean;
  search?: string;
  limit?: number;
} = {}): Promise<Thread[]> {
  const query = new URLSearchParams();
  if (params.projectId !== undefined) {
    query.set("projectId", params.projectId === null ? "null" : params.projectId);
  }
  if (params.starred !== undefined) {
    query.set("starred", String(params.starred));
  }
  if (params.archived !== undefined) {
    query.set("archived", String(params.archived));
  }
  if (params.search !== undefined && params.search !== "") {
    query.set("search", params.search);
  }
  if (params.limit !== undefined) {
    query.set("limit", String(params.limit));
  }
  const suffix = query.toString() === "" ? "" : `?${query.toString()}`;
  const response = await fetch(`/api/threads${suffix}`);
  return expectJSON<Thread[]>(response, "failed to load threads");
}

export async function createThread(input: { projectId?: string | null; title?: string } = {}): Promise<Thread> {
  const response = await fetch("/api/threads", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return expectJSON<Thread>(response, "failed to create thread");
}

export async function getThread(threadId: string): Promise<ThreadResponse> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}`);
  return expectJSON<ThreadResponse>(response, "failed to load thread");
}

export async function setThreadStarred(threadId: string, starred: boolean): Promise<Thread> {
  const action = starred ? "star" : "unstar";
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/${action}`, {
    method: "POST",
  });
  return expectJSON<Thread>(response, "failed to update thread");
}

export async function updateThread(threadId: string, input: { title?: string }): Promise<Thread> {
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

export async function bulkDeleteThreads(threadIds: string[]): Promise<{ deleted: number }> {
  const response = await fetch("/api/threads:delete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ threadIds }),
  });
  return expectJSON<{ deleted: number }>(response, "failed to delete threads");
}

export async function stopMessage(threadId: string): Promise<void> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/messages:stop`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to stop message");
  }
}

export async function downloadArtifact(downloadUrl: string): Promise<Blob> {
  const response = await fetch(downloadUrl);
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to download artifact");
  }
  return response.blob();
}

export async function openArtifact(downloadUrl: string): Promise<void> {
  const openUrl = artifactActionUrl(downloadUrl, "open");
  const response = await fetch(openUrl, { method: "POST" });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to open artifact");
  }
}

export async function streamMessage(
  threadId: string,
  content: string,
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/messages:stream`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ content }),
    signal,
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error(await readStreamError(response));
  }
  if (!response.body) {
    throw new Error("stream response has no body");
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      buffer = drainSSEBuffer(buffer, handlers);
    }
    buffer += decoder.decode();
    drainSSEBuffer(buffer, handlers);
  } finally {
    reader.releaseLock();
  }
}

function artifactActionUrl(downloadUrl: string, action: string): string {
  const url = new URL(downloadUrl, window.location.origin);
  if (!url.pathname.endsWith("/download")) {
    throw new Error("invalid artifact download URL");
  }
  url.pathname = `${url.pathname.slice(0, -"/download".length)}/${action}`;
  if (url.origin === window.location.origin) {
    return `${url.pathname}${url.search}`;
  }
  return url.toString();
}

async function readStreamError(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { error?: unknown };
    if (typeof body.error === "string" && body.error !== "") {
      return body.error;
    }
  } catch {
    // response body was empty or not JSON
  }
  return "failed to stream message";
}

async function expectJSON<T>(response: Response, errorMessage: string): Promise<T> {
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error(errorMessage);
  }
  return response.json() as Promise<T>;
}

function drainSSEBuffer(buffer: string, handlers: StreamHandlers): string {
  let separator = buffer.indexOf("\n\n");
  while (separator !== -1) {
    const rawEvent = buffer.slice(0, separator);
    buffer = buffer.slice(separator + 2);
    dispatchSSEEvent(rawEvent, handlers);
    separator = buffer.indexOf("\n\n");
  }
  return buffer;
}

function dispatchSSEEvent(rawEvent: string, handlers: StreamHandlers) {
  let event = "";
  const dataLines: string[] = [];
  for (const line of rawEvent.split("\n")) {
    if (line.startsWith("event:")) {
      event = line.slice("event:".length).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trim());
    }
  }
  if (event === "" || dataLines.length === 0) {
    return;
  }
  const payload = JSON.parse(dataLines.join("\n")) as unknown;
  switch (event) {
    case "user_message":
      handlers.onUserMessage(payload as Message);
      break;
    case "assistant_delta":
      handlers.onDelta((payload as { content: string }).content);
      break;
    case "assistant_reasoning_delta":
      handlers.onReasoningDelta?.((payload as { content: string }).content);
      break;
    case "assistant_message":
      handlers.onAssistantMessage(payload as Message);
      break;
    case "thread":
      handlers.onThread(payload as Thread);
      break;
    case "tool_call":
      handlers.onToolCall?.(payload as ToolCallEvent);
      break;
    case "tool_result":
      handlers.onToolResult?.(payload as ToolResultEvent);
      break;
    case "mcp_status":
      handlers.onMcpStatus?.(payload as McpStatusEvent);
      break;
    case "artifact":
      handlers.onArtifact?.(payload as Artifact);
      break;
    case "done":
      break;
    case "error":
      throw new Error((payload as { error?: string }).error ?? "stream failed");
  }
}
