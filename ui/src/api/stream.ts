import { AuthExpiredError } from "./http";
import type {
  Artifact,
  Citation,
  Message,
  MessagePastedText,
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
  // Web sources gathered so far this turn, re-sent after every tool round so
  // inline [n] markers resolve while the answer is still streaming. Always a full
  // snapshot — replace, do not merge.
  onWebSources?(sources: Citation[]): void;
};

// StreamInterruptedError: the connection closed (a proxy timeout, a dropped
// network, the server dying) before the turn reached a terminal event
// (`assistant_message`, `done` or `error`). Whatever streamed so far is real;
// the turn did not complete.
export class StreamInterruptedError extends Error {
  constructor() {
    super("stream interrupted");
    this.name = "StreamInterruptedError";
  }
}

// StreamFailedError carries the server's own `error` event text, which is
// written for the user (e.g. "image generation was not completed").
export class StreamFailedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "StreamFailedError";
  }
}

// PayloadTooLargeError: the server refused the request body outright (413).
export class PayloadTooLargeError extends Error {
  constructor() {
    super("request body too large");
    this.name = "PayloadTooLargeError";
  }
}

export async function streamMessage(
  threadId: string,
  content: string,
  handlers: StreamHandlers,
  signal?: AbortSignal,
  opts: {
    documentAttachmentIds?: string[];
    imageAttachmentIds?: string[];
    pastedTexts?: MessagePastedText[];
  } = {},
): Promise<void> {
  const requestBody: {
    content: string;
    documentAttachmentIds?: string[];
    imageAttachmentIds?: string[];
    pastedTexts?: MessagePastedText[];
  } = { content };
  if (opts.documentAttachmentIds && opts.documentAttachmentIds.length > 0) {
    requestBody.documentAttachmentIds = opts.documentAttachmentIds;
  }
  if (opts.imageAttachmentIds && opts.imageAttachmentIds.length > 0) {
    requestBody.imageAttachmentIds = opts.imageAttachmentIds;
  }
  if (opts.pastedTexts && opts.pastedTexts.length > 0) {
    requestBody.pastedTexts = opts.pastedTexts;
  }
  const response = await fetch(
    `/api/threads/${encodeURIComponent(threadId)}/messages:stream`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
      signal,
    },
  );
  await readSSEStream(await expectStreamResponse(response), handlers);
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
  const requestBody: {
    content: string;
    history: typeof history;
  } = {
    content,
    history,
  };
  const response = await fetch(`/api/incognito/messages:stream`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(requestBody),
    signal,
  });
  await readSSEStream(await expectStreamResponse(response), handlers);
}

// expectStreamResponse checks the HTTP status before any event is parsed: the
// server rejects a bad send (unknown attachment, oversized body) with a plain
// error response, never inside the event stream.
async function expectStreamResponse(
  response: Response,
): Promise<ReadableStream<Uint8Array>> {
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (response.status === 413) {
    throw new PayloadTooLargeError();
  }
  if (!response.ok) {
    throw new Error(await readStreamError(response));
  }
  if (!response.body) {
    throw new Error("stream response has no body");
  }
  return response.body;
}

// readSSEStream consumes the event stream until it ends. A stream that ends
// without a terminal event is an interruption, not a completed turn: the run
// would otherwise be dropped as if it had succeeded, taking the partial answer
// with it and showing no error.
async function readSSEStream(
  body: ReadableStream<Uint8Array>,
  handlers: StreamHandlers,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let settled = false;
  const dispatch = (rawEvent: string) => {
    if (dispatchSSEEvent(rawEvent, handlers)) settled = true;
  };
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      buffer = drainSSEBuffer(buffer, dispatch);
    }
    buffer += decoder.decode();
    drainSSEBuffer(buffer, dispatch);
  } finally {
    if (!settled) {
      // Whether the server threw an error event mid-stream or the loop is
      // unwinding for another reason, tell the body we are done with it so the
      // connection is released instead of lingering until GC.
      await reader.cancel().catch(() => {});
    }
    reader.releaseLock();
  }
  if (!settled) {
    throw new StreamInterruptedError();
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

// drainSSEBuffer dispatches every complete event in buffer and returns the
// unfinished remainder. Line endings are normalised first: the spec allows
// CRLF, and a proxy may rewrite them.
function drainSSEBuffer(
  buffer: string,
  dispatch: (rawEvent: string) => void,
): string {
  buffer = buffer.replace(/\r\n/g, "\n");
  let separator = buffer.indexOf("\n\n");
  while (separator !== -1) {
    const rawEvent = buffer.slice(0, separator);
    buffer = buffer.slice(separator + 2);
    dispatch(rawEvent);
    separator = buffer.indexOf("\n\n");
  }
  return buffer;
}

// dispatchSSEEvent routes one event to its handler and reports whether it was
// a terminal event for the turn.
function dispatchSSEEvent(rawEvent: string, handlers: StreamHandlers): boolean {
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
    return false;
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
      return true;
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
      handlers.onKnowledgeSources?.(
        (payload as { sources: Citation[] }).sources,
      );
      break;
    case "web_sources":
      handlers.onWebSources?.((payload as { sources: Citation[] }).sources);
      break;
    case "done":
      return true;
    case "error":
      throw new StreamFailedError(
        (payload as { error?: string }).error ?? "stream failed",
      );
  }
  return false;
}
