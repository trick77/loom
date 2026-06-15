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
};

export type SidebarIconName = "chats" | "projects" | "artifacts" | "memory";
