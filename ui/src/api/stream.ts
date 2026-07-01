import { AuthExpiredError } from "./http";
import type {
  Artifact,
  Citation,
  Message,
  Thread,
  ToolCallEvent,
  ToolResultEvent,
} from "./types";

type StreamHandlers = {
  onUserMessage(message: Message): void;
  onDelta(delta: string): void;
  onReasoningDelta?(delta: string): void;
  onReasoningTitle?(event: { id: string; title: string }): void;
  onAssistantMessage(message: Message): void;
  onThread(thread: Thread): void;
  onToolPending?(): void;
  onToolCall?(event: ToolCallEvent): void;
  onToolResult?(event: ToolResultEvent): void;
  onArtifact?(artifact: Artifact): void;
  onKnowledgeSources?(sources: Citation[]): void;
};

export async function streamMessage(
  threadId: string,
  content: string,
  handlers: StreamHandlers,
  signal?: AbortSignal,
  opts: { documentAttachmentIds?: string[]; imageAttachmentIds?: string[] } = {},
): Promise<void> {
  const requestBody: {
    content: string;
    documentAttachmentIds?: string[];
    imageAttachmentIds?: string[];
  } = { content };
  if (opts.documentAttachmentIds && opts.documentAttachmentIds.length > 0) {
    requestBody.documentAttachmentIds = opts.documentAttachmentIds;
  }
  if (opts.imageAttachmentIds && opts.imageAttachmentIds.length > 0) {
    requestBody.imageAttachmentIds = opts.imageAttachmentIds;
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

// streamIncognitoMessage runs an ephemeral turn against the stateless incognito
// endpoint. The server persists nothing, so the whole prior transcript is replayed
// as `history` on every turn. Shares streamMessage's SSE plumbing.
export async function streamIncognitoMessage(
  content: string,
  history: { role: "user" | "assistant"; content: string }[],
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const response = await fetch(`/api/incognito/messages:stream`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ content, history }),
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
    case "tool_pending":
      handlers.onToolPending?.();
      break;
    case "tool_call":
      handlers.onToolCall?.(payload as ToolCallEvent);
      break;
    case "tool_result":
      handlers.onToolResult?.(payload as ToolResultEvent);
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
