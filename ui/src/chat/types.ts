import type { ActivityTraceEvent } from "../activityTrace";
import type { Message } from "../api";

export type MessageWithActivityTrace = Message & {
  activityTrace?: ActivityTraceEvent[];
  activityTraceInitiallyExpanded?: boolean;
};

export type SidebarIconName = "chats" | "projects" | "artifacts" | "memory";
