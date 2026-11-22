import { STANDARD_SLOT } from "./model";
import { AuthExpiredError, readErrorBody, UserFacingError } from "./http";
import { browserTimezone } from "./schedules";
import type {
  ActionCardData,
  Artifact,
  IdeaCardData,
  Citation,
  Message,
  MessagePastedText,
  QueuedMessage,
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
  // The part of the answer so far, stored as its own message because the turn
  // is picking up a message the user queued. The stream goes on.
  onAssistantSegment?(message: Message): void;
  // A turn failed but the stream goes on with a follow-up turn for the
  // messages queued behind it.
  onTurnError?(error: string): void;
  // The server could not answer the queued messages and hands them back.
  onQueueReturned?(queued: QueuedMessage[]): void;
  // A message's settled cost. The turn's last calls (the thread title) finish
  // after assistant_message went out, and a failed turn books its spend on the
  // user message; this is how the open thread's Σ learns either.
  onMessageCost?(event: MessageCostEvent): void;
  onThread(thread: Thread): void;
  onToolPending?(): void;
  onToolCall?(event: ToolCallEvent): void;
  onToolResult?(event: ToolResultEvent): void;
  onArtifact?(artifact: Artifact): void;
  /** An idea tool proposed something; the card asks for the user's click. */
  onIdeaCard?(card: IdeaCardData): void;
  /** A confirm server's tool call waits for the user's click on its card. */
  onActionCard?(card: ActionCardData): void;
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
    // The slot the composer shows. The server saves it before the turn
    // reads the thread, so a send right after a switch does not race the
    // switch's own save.
    chatSlot?: string;
  } = {},
): Promise<void> {
  const requestBody: {
    content: string;
    documentAttachmentIds?: string[];
    imageAttachmentIds?: string[];
    pastedTexts?: MessagePastedText[];
    clientMessageId?: string;
    chatSlot?: string;
  } = { content };
  if (opts.clientMessageId) {
    requestBody.clientMessageId = opts.clientMessageId;
  }
  if (opts.chatSlot) {
    requestBody.chatSlot = opts.chatSlot;
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
  const watch = stallWatch(
    (probe) =>
      provenBuffered(threadId, probe, { send: opts.clientMessageId ?? "" }),
    signal,
  );
  try {
    let response: Response;
    try {
      response = await fetch(
        `/api/threads/${encodeURIComponent(threadId)}/messages:stream`,
        {
          method: "POST",
          // The timezone lets a schedule created in this turn run on the
          // user's clock.
          headers: {
            "Content-Type": "application/json",
            "X-Loom-Timezone": browserTimezone(),
          },
          body: JSON.stringify(requestBody),
          signal: watch.signal,
        },
      );
    } catch (error) {
      throw watch.translate(error, () => new StreamConnectError());
    }
    await readSSEStream(await expectStreamResponse(response), handlers, watch);
  } catch (error) {
    // A stalled send throws StreamStalledError, an interruption: the caller
    // confirms the message reached the server and follows the turn, which
    // attachStream now does by polling.
    throw watch.translate(error);
  } finally {
    watch.stop();
  }
}

// attachStream reattaches to the thread's running turn: the server replays every
// event the turn has sent so far, then follows it live, so the handlers see the
// turn from its first event. "finished" means no turn is running any more; its
// answer, if any, is already saved on the thread. Once a proxy proved to buffer
// the stream (see stallWatch), it follows by polling instead.
export async function attachStream(
  threadId: string,
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<"attached" | "finished"> {
  if (buffered) return pollAttach(threadId, handlers, signal);
  const watch = stallWatch(
    (probe) => provenBuffered(threadId, probe, {}),
    signal,
  );
  try {
    let response: Response;
    try {
      response = await fetch(
        `/api/threads/${encodeURIComponent(threadId)}/messages:attach`,
        { method: "GET", signal: watch.signal },
      );
    } catch (error) {
      // fetch rejects with a TypeError when the network is down: the same
      // dropped connection as a stream cut mid-turn.
      throw watch.translate(error, () => new StreamInterruptedError());
    }
    if (response.status === 204) return "finished";
    await readSSEStream(await expectStreamResponse(response), handlers, watch);
    return "attached";
  } catch (error) {
    const translated = watch.translate(error);
    // Nothing reached the handlers yet: the poll replays the turn from its
    // start, as this attach would have.
    if (translated instanceof StreamStalledError)
      return pollAttach(threadId, handlers, signal);
    throw translated;
  } finally {
    watch.stop();
  }
}

// --- a proxy that buffers the stream ---------------------------------------
//
// A TLS-inspecting proxy (an office network) can hold every response until it
// ends: the turn's stream then arrives in one piece once the turn is over. A
// stream that sends no byte for PROBE_MS asks messages:poll whether its turn is
// registered; a turn that is, while the stream still has nothing, proves the
// proxy, and the page follows turns by polling (whole responses) from then on.
// Only proof aborts: a slow network keeps reading, and a send is never broken
// off before the server stored it (the hub registers after the store).

// HEARTBEAT_MS is the server's keepalive (streamHeartbeatInterval): a stream
// that sent a byte goes quiet for no longer, plus slack.
export const PROBE_MS = 5_000;
// A page that already proved the proxy probes a new stream sooner: its turn
// registers within moments of the store, and the stream will not come.
export const PROBE_KNOWN_MS = 1_000;
export const HEARTBEAT_MS = 20_000;
export const WATCHDOG_MS = 2 * HEARTBEAT_MS + 5_000;
// A page that slept fires its timers late and was hidden: no verdict then.
export const RESUME_MS = 10_000;
export const FROZEN_MS = 2_000;

// buffered: a proxy proved to buffer the stream, for the page's life (a reload
// probes again: a misfire must not stick).
let buffered = false;
let resumedAt = -Infinity;
if (typeof document !== "undefined") {
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) resumedAt = Date.now();
  });
}

export const streamBuffered = () => buffered;

// resetStreamTransport forgets the proof (tests).
export function resetStreamTransport() {
  buffered = false;
  resumedAt = -Infinity;
}

// StreamStalledError: a proxy holds the stream back. An interruption to every
// caller; attachStream goes on by polling.
export class StreamStalledError extends StreamInterruptedError {
  constructor() {
    super();
    this.name = "StreamStalledError";
  }
}

type StallWatch = {
  signal: AbortSignal;
  bytes(): void;
  stop(): void;
  // aborted reports that the watch itself aborted the stream.
  aborted(): boolean;
  translate(error: unknown, fallback?: () => Error): unknown;
};

// stallWatch guards one stream: its signal aborts the fetch on proof of a
// buffering proxy (StreamStalledError) or on silence past WATCHDOG_MS after the
// first byte (a plain interruption: the reattach's own watch decides then).
// The caller's signal aborts it too and stays an AbortError, a user stop. A
// probe in flight is aborted once bytes come or the stream ends: the server
// would hold it up to its poll hold.
function stallWatch(
  proof: (signal: AbortSignal) => Promise<boolean>,
  outer?: AbortSignal,
): StallWatch {
  const controller = new AbortController();
  const onOuter = () => controller.abort(outer?.reason);
  if (outer?.aborted) controller.abort(outer.reason);
  else outer?.addEventListener("abort", onOuter, { once: true });
  const probeMs = buffered ? PROBE_KNOWN_MS : PROBE_MS;
  let probe: AbortController | null = null;
  const endProbe = () => {
    probe?.abort();
    probe = null;
  };
  let outcome: "stalled" | "silent" | null = null;
  let gotBytes = false;
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const asleep = (due: number) =>
    (typeof document !== "undefined" && document.hidden) ||
    Date.now() - resumedAt < RESUME_MS ||
    Date.now() - due > FROZEN_MS;
  const arm = (ms: number) => {
    clearTimeout(timer);
    const due = Date.now() + ms;
    timer = setTimeout(() => void fire(due), ms);
  };
  const fire = async (due: number) => {
    if (stopped) return;
    // Slept: the stream may be fine, a reattach would replay the whole turn.
    if (asleep(due)) return arm(gotBytes ? WATCHDOG_MS : probeMs);
    if (gotBytes) {
      outcome = "silent";
      controller.abort();
      return;
    }
    probe = new AbortController();
    const proven = await proof(probe.signal).catch(() => false);
    probe = null;
    if (stopped || gotBytes) return;
    if (!proven) return arm(probeMs);
    outcome = "stalled";
    buffered = true;
    controller.abort();
  };
  arm(probeMs);
  return {
    signal: controller.signal,
    bytes() {
      if (stopped) return;
      gotBytes = true;
      endProbe();
      arm(WATCHDOG_MS);
    },
    stop() {
      stopped = true;
      clearTimeout(timer);
      endProbe();
      outer?.removeEventListener("abort", onOuter);
    },
    aborted: () => outcome !== null,
    translate(error, fallback) {
      if (outcome === "stalled") return new StreamStalledError();
      if (outcome === "silent") return new StreamInterruptedError();
      if (error instanceof DOMException && error.name === "AbortError")
        return error;
      return fallback ? fallback() : error;
    },
  };
}

// pollResponse is one messages:poll answer.
type PollResponse = {
  hub: string;
  sendId: string;
  next: number;
  done: boolean;
  events: { event: string; data: string }[];
};

const pollURL = (threadId: string, hub = "", after = 0) =>
  `/api/threads/${encodeURIComponent(threadId)}/messages:poll` +
  (hub ? `?hub=${encodeURIComponent(hub)}&after=${after}` : "");

// provenBuffered asks whether the stream's turn is registered and has events
// the stream would already have carried: proof, while the stream has no byte,
// that a proxy holds it. For a send it must be that send's turn (opts.send, its
// client id): a send without one is never proven, so it is never broken off
// before the server stored it. Any doubt is no proof.
async function provenBuffered(
  threadId: string,
  signal: AbortSignal,
  opts: { send?: string },
): Promise<boolean> {
  if (opts.send === "") return false;
  const response = await fetch(pollURL(threadId), { method: "GET", signal });
  if (response.status !== 200) return false;
  const body = (await response.json()) as PollResponse;
  if (body.events.length === 0) return false;
  return opts.send === undefined || body.sendId === opts.send;
}

// pollAttach follows the thread's running turn in whole responses: the first
// poll starts where an attach does, each next one at the cursor the last gave.
async function pollAttach(
  threadId: string,
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<"attached" | "finished"> {
  let hub = "";
  let next = 0;
  let settled = false;
  for (;;) {
    let response: Response;
    try {
      response = await fetch(pollURL(threadId, hub, next), {
        method: "GET",
        signal,
      });
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError")
        throw error;
      throw new StreamInterruptedError();
    }
    // Nothing runs any more: the answer is on the thread.
    if (response.status === 204) return "finished";
    if (response.status === 401) throw new AuthExpiredError();
    // 409 hub_gone, or the server in trouble: the caller retries afresh.
    if (!response.ok) throw new StreamInterruptedError();
    let body: PollResponse;
    try {
      body = (await response.json()) as PollResponse;
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError")
        throw error;
      throw new StreamInterruptedError();
    }
    hub = body.hub;
    next = body.next;
    for (const e of body.events) {
      const outcome = dispatchEvent(e.event, e.data, handlers);
      if (outcome === "reopened") settled = false;
      else if (outcome) settled = true;
    }
    if (body.done) {
      if (!settled) throw new StreamInterruptedError();
      return "attached";
    }
  }
}

// streamIncognitoMessage runs an ephemeral turn against the stateless incognito
// endpoint. The server persists nothing, so the whole prior transcript is replayed
// as `history` on every turn. Shares streamMessage's SSE plumbing.
export async function streamIncognitoMessage(
  content: string,
  history: { role: "user" | "assistant"; content: string; model?: string }[],
  handlers: StreamHandlers,
  signal?: AbortSignal,
  chatSlot?: string,
): Promise<void> {
  const requestBody: {
    content: string;
    history: typeof history;
    chatSlot?: string;
  } = {
    content,
    history,
  };
  // Only a non-default pick is sent: an older backend 400s an unknown field.
  if (chatSlot !== undefined && chatSlot !== STANDARD_SLOT) {
    requestBody.chatSlot = chatSlot;
  }
  const response = await fetch(`/api/incognito/messages:stream`, {
    method: "POST",
    // The prompt's date reads in the user's zone.
    headers: {
      "Content-Type": "application/json",
      "X-Loom-Timezone": browserTimezone(),
    },
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
    const data = await readErrorBody(response);
    throw new UserFacingError(data.error ?? "failed to stream message");
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
  watch?: StallWatch,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let settled = false;
  const dispatch = (rawEvent: string) => {
    const outcome = dispatchSSEEvent(rawEvent, handlers);
    // A follow-up turn on the same stream opens with its user message: until
    // it settles in turn, a drop is an interruption again.
    if (outcome === "reopened") settled = false;
    else if (outcome) settled = true;
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
        // The watch giving up on a stream that went quiet after the answer
        // arrived ends it like a dropped one: no failure.
        if (settled && watch?.aborted()) break;
        if (error instanceof DOMException && error.name === "AbortError")
          throw error;
        if (settled) break;
        throw new StreamInterruptedError();
      }
      const { value, done } = chunk;
      if (done) {
        break;
      }
      // Any byte, a keepalive comment too, shows the stream is not held back.
      watch?.bytes();
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

// dispatchSSEEvent parses one event off the stream and dispatches it.
function dispatchSSEEvent(
  rawEvent: string,
  handlers: StreamHandlers,
): boolean | "reopened" {
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
  return dispatchEvent(event, dataLines.join("\n"), handlers);
}

// dispatchEvent routes one event, from the stream or a poll, to its handler and
// reports whether it was a terminal event for the turn, or a user message
// opening a turn.
function dispatchEvent(
  event: string,
  data: string,
  handlers: StreamHandlers,
): boolean | "reopened" {
  const payload = JSON.parse(data) as unknown;
  switch (event) {
    case "user_message":
      handlers.onUserMessage(payload as Message);
      return "reopened";
    case "queue_returned":
      handlers.onQueueReturned?.(
        (payload as { queued?: QueuedMessage[] }).queued ?? [],
      );
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
    case "assistant_segment":
      handlers.onAssistantSegment?.(payload as Message);
      break;
    case "turn_error":
      handlers.onTurnError?.((payload as { error?: string }).error ?? "");
      break;
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
    case "idea_card":
      handlers.onIdeaCard?.(payload as IdeaCardData);
      break;
    case "action_card":
      handlers.onActionCard?.(payload as ActionCardData);
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
