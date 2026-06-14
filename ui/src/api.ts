import type { ActivityTraceEvent } from "./activityTrace";

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
  starred: boolean;
  archivedAt?: string;
  createdAt: string;
  updatedAt: string;
};

export type ProjectMemory = {
  projectId: string;
  content: string;
  updatedAt: string | null;
};

export type UserMemory = {
  content: string;
  updatedAt: string | null;
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
  activityTrace?: ActivityTraceEvent[];
  artifacts?: Artifact[];
  citations?: Citation[];
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
  threadId?: string;
  displayFilename: string;
  mimeType: string;
  sizeBytes: number;
  projectId?: string;
  modifiedAt?: string;
  downloadUrl: string;
  model?: string;
  provider?: string;
  width?: number;
  height?: number;
  durationMs?: number;
};

// Citation mirrors a backend RAG source: one per retrieved chunk. The UI groups
// these by filename for display (AnythingLLM-style "combine like sources").
export type Citation = {
  documentId: string;
  filename: string;
  snippet: string;
  score: number;
};

// Document is an uploaded file tracked for retrieval-augmented generation.
export type Document = {
  id: string;
  filename: string;
  mimeType: string;
  sizeBytes: number;
  status: "pending" | "extracting" | "embedding" | "embedded" | "stale" | "error";
  error?: string;
  projectId?: string;
  artifactId?: string;
  createdAt: string;
};

// Extensions accepted for document upload — keep in sync with the backend
// allowlist (internal/documents/allowlist.go).
export const DOCUMENT_ACCEPT =
  ".pdf,.docx,.pptx,.xlsx,.txt,.md,.csv,.json,.html";
export const IMAGE_ATTACHMENT_ACCEPT = ".png,.jpg,.jpeg,.webp,.gif";
export const ATTACHMENT_ACCEPT = `${DOCUMENT_ACCEPT},${IMAGE_ATTACHMENT_ACCEPT}`;
export const DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE = 5;
export const DOCUMENT_MAX_CHAT_ATTACHMENTS = 10;
export const DOCUMENT_MAX_UPLOAD_BYTES = 25 * 1024 * 1024;

export type ArtifactListType = "all" | "images" | "files";
export type ArtifactSort = "name" | "modified" | "size";
export type SortOrder = "asc" | "desc";

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
  servers?: { name: string; active: boolean }[];
};

type ThreadResponse = {
  thread: Thread;
  messages: Message[];
};

type StreamHandlers = {
  onUserMessage(message: Message): void;
  onDelta(delta: string): void;
  onReasoningDelta?(delta: string): void;
  onReasoningTitle?(event: { id: string; title: string }): void;
  onAssistantMessage(message: Message): void;
  onThread(thread: Thread): void;
  onProject?(project: Project): void;
  onToolPending?(): void;
  onToolCall?(event: ToolCallEvent): void;
  onToolResult?(event: ToolResultEvent): void;
  onMcpStatus?(event: McpStatusEvent): void;
  onArtifact?(artifact: Artifact): void;
  onKnowledgeSources?(sources: Citation[]): void;
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

export async function updateProject(
  projectId: string,
  input: { name?: string; description?: string },
): Promise<Project> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return expectJSON<Project>(response, "failed to update project");
}

export async function setProjectStarred(projectId: string, starred: boolean): Promise<Project> {
  const action = starred ? "star" : "unstar";
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/${action}`, {
    method: "POST",
  });
  return expectJSON<Project>(response, "failed to update project");
}

export async function archiveProject(projectId: string): Promise<void> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/archive`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to archive project");
  }
}

export async function deleteProject(projectId: string): Promise<void> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}`, {
    method: "DELETE",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to delete project");
  }
}

export async function getProjectMemory(projectId: string): Promise<ProjectMemory> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/memory`);
  return expectJSON<ProjectMemory>(response, "failed to load project memory");
}

export async function refreshProjectMemory(projectId: string): Promise<ProjectMemory> {
  const response = await fetch(`/api/projects/${encodeURIComponent(projectId)}/memory:refresh`, {
    method: "POST",
  });
  return expectJSON<ProjectMemory>(response, "failed to refresh project memory");
}

export async function getUserMemory(): Promise<UserMemory> {
  const response = await fetch(`/api/me/memory`);
  return expectJSON<UserMemory>(response, "failed to load user memory");
}

export type Usage = {
  promptTokens: number;
  completionTokens: number;
  cachedTokens: number;
  reasoningTokens: number;
  totalTokens: number;
  embeddingTokens: number;
  embeddingRequests: number;
  webSearches: number;
  webFetches: number;
  obscuraFetches: number;
  imageGens: number;
  chatsCreated: number;
  projectsCreated: number;
  userMemoryLength: number;
  userMemoryMax: number;
};

export async function getUsage(): Promise<Usage> {
  const response = await fetch(`/api/me/usage`);
  return expectJSON<Usage>(response, "failed to load usage");
}

// Page is the cursor-pagination envelope returned by list endpoints.
// nextCursor is null when there are no further pages.
export type Page<T> = {
  items: T[];
  nextCursor: string | null;
};

export async function listThreads(params: {
  projectId?: string | null;
  starred?: boolean;
  archived?: boolean;
  search?: string;
  limit?: number;
  cursor?: string | null;
} = {}): Promise<Page<Thread>> {
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
  if (params.cursor !== undefined && params.cursor !== null && params.cursor !== "") {
    query.set("cursor", params.cursor);
  }
  const suffix = query.toString() === "" ? "" : `?${query.toString()}`;
  const response = await fetch(`/api/threads${suffix}`);
  return expectJSON<Page<Thread>>(response, "failed to load threads");
}

// listThreadIds returns the ids of every thread matching the search, with no
// pagination — used by "select all matches" so the client can act on threads
// it has not loaded into the list.
export async function listThreadIds(params: { search?: string } = {}): Promise<string[]> {
  const query = new URLSearchParams();
  if (params.search !== undefined && params.search !== "") {
    query.set("search", params.search);
  }
  const suffix = query.toString() === "" ? "" : `?${query.toString()}`;
  const response = await fetch(`/api/threads/ids${suffix}`);
  return expectJSON<string[]>(response, "failed to load thread ids");
}

export async function listArtifacts(params: {
  type?: ArtifactListType;
  sort?: ArtifactSort;
  order?: SortOrder;
  search?: string;
  limit?: number;
  cursor?: string | null;
} = {}): Promise<Page<Artifact>> {
  const query = new URLSearchParams();
  if (params.type !== undefined) {
    query.set("type", params.type);
  }
  if (params.sort !== undefined) {
    query.set("sort", params.sort);
  }
  if (params.order !== undefined) {
    query.set("order", params.order);
  }
  if (params.search !== undefined && params.search !== "") {
    query.set("search", params.search);
  }
  if (params.limit !== undefined) {
    query.set("limit", String(params.limit));
  }
  if (params.cursor !== undefined && params.cursor !== null && params.cursor !== "") {
    query.set("cursor", params.cursor);
  }
  const suffix = query.toString() === "" ? "" : `?${query.toString()}`;
  const response = await fetch(`/api/artifacts${suffix}`);
  return expectJSON<Page<Artifact>>(response, "failed to load artifacts");
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

export async function updateThread(
  threadId: string,
  input: { title?: string; projectId?: string | null },
): Promise<Thread> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return expectJSON<Thread>(response, "failed to update thread");
}

export async function archiveThread(threadId: string): Promise<void> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/archive`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to archive thread");
  }
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

export async function uploadDocument(
  file: File,
  opts: { threadId?: string; projectId?: string } = {},
): Promise<Document> {
  const form = new FormData();
  form.append("file", file);
  if (opts.threadId) form.append("threadId", opts.threadId);
  if (opts.projectId) form.append("projectId", opts.projectId);
  const response = await fetch("/api/documents/upload", { method: "POST", body: form });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (response.status === 415) {
    throw new Error("Unsupported document format");
  }
  if (response.status === 409) {
    throw new Error(`A chat can have up to ${DOCUMENT_MAX_CHAT_ATTACHMENTS} attached files.`);
  }
  if (response.status === 413) {
    throw new Error("Files must be 25 MB or smaller.");
  }
  return expectJSON<Document>(response, "failed to upload document");
}

export async function uploadImageAttachment(
  file: File,
  opts: { threadId?: string; projectId?: string } = {},
): Promise<Artifact> {
  const form = new FormData();
  form.append("file", file);
  if (opts.threadId) form.append("threadId", opts.threadId);
  if (opts.projectId) form.append("projectId", opts.projectId);
  const response = await fetch("/api/artifacts/images/upload", { method: "POST", body: form });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (response.status === 415) {
    throw new Error("Unsupported image format");
  }
  if (response.status === 409) {
    throw new Error(`A chat can have up to ${DOCUMENT_MAX_CHAT_ATTACHMENTS} attached files.`);
  }
  if (response.status === 413) {
    throw new Error("Files must be 25 MB or smaller.");
  }
  return expectJSON<Artifact>(response, "failed to upload image");
}

export async function listDocuments(projectId?: string): Promise<Document[]> {
  const suffix = projectId ? `?projectId=${encodeURIComponent(projectId)}` : "";
  const response = await fetch(`/api/documents${suffix}`);
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  const body = await expectJSON<{ items: Document[] }>(response, "failed to load documents");
  return body.items ?? [];
}

export async function indexDocument(documentId: string): Promise<Document> {
  const response = await fetch(`/api/documents/${encodeURIComponent(documentId)}/index`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  return expectJSON<Document>(response, "failed to index document");
}

export async function unindexDocument(documentId: string): Promise<void> {
  const response = await fetch(`/api/documents/${encodeURIComponent(documentId)}/unindex`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to unindex document");
  }
}

export async function deleteDocument(documentId: string): Promise<void> {
  const response = await fetch(`/api/documents/${encodeURIComponent(documentId)}`, {
    method: "DELETE",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to delete document");
  }
}

export async function streamMessage(
  threadId: string,
  content: string,
  handlers: StreamHandlers,
  signal?: AbortSignal,
  opts: { imageAttachmentIds?: string[]; documentAttachmentIds?: string[] } = {},
): Promise<void> {
  const requestBody: {
    content: string;
    imageAttachmentIds?: string[];
    documentAttachmentIds?: string[];
  } = { content };
  if (opts.imageAttachmentIds && opts.imageAttachmentIds.length > 0) {
    requestBody.imageAttachmentIds = opts.imageAttachmentIds;
  }
  if (opts.documentAttachmentIds && opts.documentAttachmentIds.length > 0) {
    requestBody.documentAttachmentIds = opts.documentAttachmentIds;
  }
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/messages:stream`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(requestBody),
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
    case "assistant_reasoning_title":
      handlers.onReasoningTitle?.(payload as { id: string; title: string });
      break;
    case "assistant_message":
      handlers.onAssistantMessage(payload as Message);
      break;
    case "thread":
      handlers.onThread(payload as Thread);
      break;
    case "project":
      handlers.onProject?.(payload as Project);
      break;
    case "tool_pending":
      handlers.onToolPending?.();
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
    case "knowledge_sources":
      handlers.onKnowledgeSources?.((payload as { sources: Citation[] }).sources);
      break;
    case "done":
      break;
    case "error":
      throw new Error((payload as { error?: string }).error ?? "stream failed");
  }
}
