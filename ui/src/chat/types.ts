import type { ActivityTraceEvent } from "../activityTrace";
import type { Message } from "../api";
import type { ComposerAttachment } from "./useDocumentAttachments";

// MessageWithActivityTrace is the rendered/stateful message: a Message plus the
// UI-side attachments. The persisted wire attachments (LoadedMessage) are
// rehydrated into ComposerAttachment[] at load, so a sent message looks identical
// whether it was just sent (live composer attachments) or restored on reload.
export type MessageWithActivityTrace = Message & {
  activityTrace?: ActivityTraceEvent[];
  attachments?: ComposerAttachment[];
  // Stable React key for an optimistically-sent message. The bubble is rendered
  // before the server assigns an id and later reconciled to the persisted message;
  // keying off this client id (instead of the swapping `id`) keeps the same DOM node
  // across that reconcile, avoiding an unmount/remount (and scroll jump) when the
  // server's `user_message` event arrives — which on buffering networks is at the
  // very end of the turn. Absent on messages loaded from the server.
  clientKey?: string;
};

export type SidebarIconName = "threads" | "projects" | "artifacts" | "memory";
