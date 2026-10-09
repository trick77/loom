import { AuthExpiredError, UserFacingError } from "./http";
import type {
  Artifact,
  Citation,
  Message,
  MessagePastedText,
  Thread,
  ToolCallEvent,
  ToolResultEvent,
} from "./types";

export type MessageCostEvent = { id: string; costNanoUsd: number };

export type StreamHandlers = {
  onUserMessage(message: Message): void;
  onDelta(delta: string): void;
  onReasoningDelta?(delta: string): void;
  onReasoningTitle?(event: { id: string; title: string }): void;
  // The turn's first sweep line, generated from the user's message while the
  // answer is prepared; shown until the first reasoning title replaces it.
  onWorkingTitle?(title: string): void;
  onAssistantMessage(message: Message): void;
  // A message's settled cost. The turn's last calls (the thread title) finish
  // after assistant_message went out, and a failed turn books its spend on the
  // user message; this is how the open thread's Σ learns either.
  onMessageCost?(event: MessageCostEvent): void;
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

// StreamConnectError: the send request itself failed on the network (fetch
// rejected), so no response arrived. The server may still have stored the
// message; the caller asks before treating the send as failed.
export class StreamConnectError extends Error {
  constructor() {
    super("stream request failed");
    this.name = "StreamConnectError";
  }
}

// StreamFailedError carries the server's own `error` event text, which is
// written for the user (e.g. "image generation was not completed").
export class StreamFailedError extends UserFacingError {
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
    // The client's id for this send: stored with the message, named by a stop.
    clientMessageId?: string;
  } = {},
): Promise<void> {
  const requestBody: {
    content: string;
    documentAttachmentIds?: string[];
    imageAttachmentIds?: string[];
    pastedTexts?: MessagePastedText[];
    clientMessageId?: string;
  } = { content };
  if (opts.clientMessageId) {
    requestBody.clientMessageId = opts.clientMessageId;
  }
  if (opts.documentAttachmentIds && opts.documentAttachmentIds.length > 0) {
    requestBody.documentAttachmentIds = opts.documentAttachmentIds;
  }
  if (opts.imageAttachmentIds && opts.imageAttachmentIds.length > 0) {
    requestBody.imageAttachmentIds = opts.imageAttachmentIds;
  }
  if (opts.pastedTexts && opts.pastedTexts.length > 0) {
    requestBody.pastedTexts = opts.pastedTexts;
  }
  let response: Response;
  try {
    response = await fetch(
      `/api/threads/${encodeURIComponent(threadId)}/messages:stream`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(requestBody),
        signal,
      },
    );
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError")
      throw error;
    throw new StreamConnectError();
  }
  await readSSEStream(await expectStreamResponse(response), handlers);
}

// attachStream reattaches to the thread's running turn: the server replays every
// event the turn has sent so far, then follows it live, so the handlers see the
// turn from its first event. "finished" means no turn is running any more; its
// answer, if any, is already saved on the thread.
export async function attachStream(
  threadId: string,
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<"attached" | "finished"> {
  let response: Response;
  try {
    response = await fetch(
      `/api/threads/${encodeURIComponent(threadId)}/messages:attach`,
      { method: "GET", signal },
    );
  } catch (error) {
    // fetch rejects with a TypeError when the network is down: the same
    // dropped connection as a stream cut mid-turn.
    if (error instanceof DOMException && error.name === "AbortError")
      throw error;
    throw new StreamInterruptedError();
  }
  if (response.status === 204) return "finished";
  await readSSEStream(await expectStreamResponse(response), handlers);
  return "attached";
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
    // The server rejects a send before the stream opens with a message meant
    // for the user (an unknown attachment, content that is too long).
    throw new UserFacingError(await readStreamError(response));
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
      let chunk: ReadableStreamReadResult<Uint8Array>;
      try {
        chunk = await reader.read();
      } catch (error) {
        // Our own abort stays an AbortError. Anything else is the connection
        // going away (Safari: TypeError "Load failed"), typically a phone
        // freezing the tab: after the answer arrived that changes nothing,
        // before it the turn was interrupted, not failed to send.
        if (error instanceof DOMException && error.name === "AbortError")
          throw error;
        if (settled) break;
        throw new StreamInterruptedError();
      }
      const { value, done } = chunk;
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
    case "assistant_working_title":
      handlers.onWorkingTitle?.((payload as { title: string }).title);
      break;
    case "assistant_message":
      handlers.onAssistantMessage(payload as Message);
      return true;
    case "message_cost":
      handlers.onMessageCost?.(payload as MessageCostEvent);
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
