import type { ActivityTraceEvent } from "../activityTrace";
import type { Message } from "../api";
import type { ComposerAttachment } from "./useDocumentAttachments";

export type MessageWithActivityTrace = Message & {
  activityTrace?: ActivityTraceEvent[];
  attachments?: ComposerAttachment[];
};

export type SidebarIconName = "chats" | "projects" | "artifacts" | "memory";
