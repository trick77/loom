import type { Idea } from "../api";

export function makeIdea(overrides: Partial<Idea> = {}): Idea {
  return {
    id: "i1",
    state: "open",
    title: "Vibe Kanban",
    reason: "Topic for the AI lab week, submit by 12 January.",
    excerpt: "Vibe Kanban is an open-source board.",
    threadId: "t1",
    messageId: "m1",
    threadTitle: "Tech news week 45",
    threadDeleted: false,
    dueOn: null,
    remindOn: null,
    doneAt: null,
    createdAt: new Date(Date.now() - 2 * 24 * 3600 * 1000).toISOString(),
    ...overrides,
  };
}
