import type { Thread } from "../api";

export function upsertThreadById(current: Thread[], thread: Thread): Thread[] {
  return [thread, ...current.filter((item) => item.id !== thread.id)];
}

export function replaceThreadById(current: Thread[], thread: Thread): Thread[] {
  return current.map((item) => (item.id === thread.id ? thread : item));
}

export function removeThreadsById(current: Thread[], threadIds: Set<string>): Thread[] {
  return current.filter((thread) => !threadIds.has(thread.id));
}
