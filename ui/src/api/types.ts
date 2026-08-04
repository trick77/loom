import type { ActivityTraceEvent } from "../activityTrace";

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
  // Backend serializes this as null (not omitted) for active projects, so
  // presence checks must use `!= null`, not `!== undefined`.
  archivedAt?: string | null;
  createdAt: string;
  updatedAt: string;
  // Last *user* activity in the project (new message, new thread, name/description
  // edit). Drives the card's "Updated X ago" label and the "Recent activity" sort.
  lastActivityAt: string;
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

/**
 * UserDirective is one explicit, user-steered standing instruction ("always
 * answer in metric units"). It is managed by the assistant via chat tools and
 * shown read-only in the UI.
 */
export type UserDirective = {
  id: string;
  content: string;
  createdAt: string;
  updatedAt: string;
};

export type Thread = {
  id: string;
  projectId?: string;
  title: string;
  /** Prompt-classifier category chosen on the first message; "" until classified. */
  category?: string;
  starred: boolean;
  /** True when an active public share link exists; set on list payloads to badge shared threads. */
  shared?: boolean;
  archivedAt?: string;
  createdAt: string;
  updatedAt: string;
  lastMessageAt?: string;
};

// ContentBlock is one ordered piece of an assistant message: a run of prose, a
// contiguous reasoning+tool run (rendered as a single collapsible activity
// panel), or a generated artifact. The backend persists assistant messages as an
// ordered ContentBlock[] so text, tool activity and images render in the exact
// chronological order they arrived; the live stream reconstructs the same order.
export type ContentBlock =
  | { type: "text"; content: string }
  | { type: "trace"; events: ActivityTraceEvent[] }
  | { type: "artifact"; artifact: Artifact };

export type Message = {
  id: string;
  threadId: string;
  role: "user" | "assistant" | "tool";
  content: string;
  reasoningContent?: string;
  activityTrace?: ActivityTraceEvent[];
  contentBlocks?: ContentBlock[];
  artifacts?: Artifact[];
  citations?: Citation[];
  createdAt: string;
  promptTokens?: number;
  completionTokens?: number;
  totalTokens?: number;
  cachedTokens?: number;
  reasoningTokens?: number;
  // contextTokens is the final answer call's model-reported total_tokens — the
  // real size of that single generation's context — used for the context-window
  // percentage. Unlike totalTokens (summed across every call in the turn), it is
  // not double-counted. Absent on messages predating the field.
  contextTokens?: number;
  durationMs?: number;
  model?: string;
  reasoningEffort?: string;
  // pastedTexts are large blocks the user pasted into the composer, collapsed into
  // "Pasted" chips. Their text is also folded into content (so the model sees it);
  // the sent bubble strips it back out and renders a chip. Absent/[] when none.
  pastedTexts?: MessagePastedText[];
};

// MessagePastedText is the persisted shape of one collapsed paste block on a
// message (mirrors the backend chat.MessagePastedText). It travels on the base
// Message so the sent bubble renders a chip whether just-sent or reloaded.
export type MessagePastedText = {
  text: string;
  lineCount: number;
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
  // thumbnailUrl points at a small JPEG preview for raster image artifacts; absent
  // for SVGs and non-images, so callers fall back to downloadUrl / a typed icon.
  thumbnailUrl?: string;
  model?: string;
  provider?: string;
  width?: number;
  height?: number;
  durationMs?: number;
  // deleted is true when the artifact has been removed from the Artifacts
  // library. Set by the read-time overlay on artifacts embedded in chat messages
  // so the chat can render a tombstone; never set on live library listings.
  deleted?: boolean;
};

// MessageAttachment is the persisted shape of one image or document a user sent
// with a message (mirrors the backend MessageAttachment). downloadUrl is set for
// images (derived from the artifact id) and absent for documents. It travels on a
// loaded message (see LoadedMessage) and is rehydrated into the richer
// ComposerAttachment the sent-message renderer uses.
export type MessageAttachment = {
  kind: "image" | "document";
  artifactId?: string;
  documentId?: string;
  filename: string;
  mimeType: string;
  sizeBytes: number;
  downloadUrl?: string;
  // thumbnailUrl is the small-preview source for raster image attachments (absent
  // for documents, SVGs, and older rows), with downloadUrl as the fallback.
  thumbnailUrl?: string;
};

// LoadedMessage is a message as returned by the thread-load endpoint: a Message
// plus the persisted attachments the user sent with it. The base Message stays
// attachment-free so the rendered/stateful MessageWithActivityTrace (which
// carries ComposerAttachment[]) remains assignable to Message everywhere.
export type LoadedMessage = Message & {
  attachments?: MessageAttachment[];
};

// Citation mirrors a backend RAG source: one per retrieved chunk. The UI groups
// these by filename for display (AnythingLLM-style "combine like sources").
export type Citation = {
  documentId: string;
  filename: string;
  snippet: string;
  score: number;
  // full marks a source whose entire document was injected (not a retrieved
  // excerpt), so the UI labels it "full document" instead of "N excerpts".
  full?: boolean;
  // index is the [n] marker the model cites inline, carried by both kinds of
  // source: uploaded documents take [1]..[k] and web sources continue after them,
  // one sequence. url is the source link and is set only for web-search citations
  // (Tavily/fetch/obscura) — the UI treats a citation with a url as a web source
  // and (for those) uses filename as the display label (the site name). Documents
  // persisted before they were numbered leave index undefined.
  url?: string;
  index?: number;
  // title (article/page title) and favicon (source-provided icon url) feed the
  // sources sidebar; snippet carries what the source delivered. Web sources only.
  title?: string;
  favicon?: string;
};

// Document is an uploaded file tracked for retrieval-augmented generation.
export type Document = {
  id: string;
  filename: string;
  mimeType: string;
  sizeBytes: number;
  status:
    "pending" | "extracting" | "embedding" | "embedded" | "stale" | "error";
  error?: string;
  projectId?: string;
  artifactId?: string;
  downloadUrl?: string;
  createdAt: string;
};

// Extensions accepted for document/knowledge upload — keep in sync with the
// backend allowlist (internal/documents/allowlist.go). Images are described via
// the vision model at ingest.
export const DOCUMENT_ACCEPT =
  ".pdf,.docx,.pptx,.xlsx,.txt,.md,.csv,.json,.html,.png,.jpg,.jpeg,.webp,.gif";
// DOCUMENT_ACCEPT already includes images, so it covers every composer attachment.
export const ATTACHMENT_ACCEPT = DOCUMENT_ACCEPT;
export const DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE = 5;
export const DOCUMENT_MAX_THREAD_ATTACHMENTS = 10;
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

export type ThreadResponse = {
  thread: Thread;
  messages: LoadedMessage[];
  /** Present once the thread has been shared; drives the Share button state + badge. */
  share?: ShareInfo;
};

// ShareInfo is the owner-facing share state for a thread.
export type ShareInfo = {
  shareId: string;
  shareUrl: string;
  shared: boolean;
  snapshotAt: string;
};

// ShareListItem is one row in the settings "Shared chats" dashboard.
export type ShareListItem = ShareInfo & {
  threadId: string;
  title: string;
};

// PublicShare is the frozen snapshot served to anonymous viewers. It is a
// sanitized subset of a thread — no sidebar/project/system data, no uploaded
// files, no citations/traces/tokens (see backend share_snapshot.go).
export type PublicShareMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  artifacts?: Artifact[];
  contentBlocks?: ContentBlock[];
  /** True when the original message carried an uploaded file (stripped from the share). */
  hadAttachment?: boolean;
  /**
   * Web sources only, field-projected by the backend's share whitelist. RAG document
   * citations never appear here — they name the owner's private uploads — and the
   * markers that cited them are stripped from the prose, so every [n] that reaches a
   * share resolves.
   */
  citations?: Citation[];
  createdAt: string;
};

export type PublicShare = {
  shareId: string;
  title: string;
  author: string;
  sharedAt: string;
  messages: PublicShareMessage[];
};

// Page is the cursor-pagination envelope returned by list endpoints.
// nextCursor is null when there are no further pages.
export type Page<T> = {
  items: T[];
  nextCursor: string | null;
};

// ThreadContentHit is one full-text content match: the matching thread plus a
// match-centered snippet with the hits wrapped in « » (see renderSnippet).
export type ThreadContentHit = { thread: Thread; snippet: string };

// MCPServerStatus is one row of the /mcp slash command: a configured MCP
// server's live reachability plus metadata. endpoint is credential-free (host
// only). error carries the probe failure reason when active is false.
export type MCPServerStatus = {
  name: string;
  active: boolean;
  transport: string;
  endpoint: string;
  origin: string; // "built-in" | "file"
  toolCount: number;
  error?: string;
};

// MCPToolInfo is one row of the /tools slash command. required is the tool's
// required arguments (serialized as null when the tool declares none).
export type MCPToolInfo = {
  name: string;
  server: string;
  description: string;
  required: string[] | null;
};

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
  threadsCreated: number;
  projectsCreated: number;
  userMemoryLength: number;
  userMemoryMax: number;
  userMemoryUpdatedAt: string | null;
  userMemorySourceMessages: number;
  userMemoryTotalMessages: number;
  userMemoryRefreshWindowHours: number;
  userDirectivesCount: number;
  userDirectivesLength: number;
  userDirectivesMax: number;
};
