import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import type { Thread } from "../api";
import { ThreadPanel } from "./ThreadPanel";
import type { MessageWithActivityTrace } from "./types";

function panel(thread: Thread, messages: MessageWithActivityTrace[]) {
  const handlers = {
    onOpenSchedule: vi.fn(),
  };
  render(
    <ThreadPanel
      thread={thread}
      threadProject={null}
      share={null}
      onShareChange={vi.fn()}
      deferredAttachNote=""
      messages={messages}
      draft=""
      streamingBlocks={[]}
      streamingSources={[]}
      toolPending={false}
      sendError=""
      isSending={false}
      openThreadMenuID={null}
      onDraftChange={vi.fn()}
      pastedTexts={[]}
      onAddPastedText={vi.fn()}
      onRemovePastedText={vi.fn()}
      onSend={vi.fn()}
      onStop={vi.fn()}
      onRetry={vi.fn()}
      onOpenProject={vi.fn()}
      onDeleteThread={vi.fn()}
      onRenameThread={vi.fn()}
      onStarThread={vi.fn()}
      onToggleThreadMenu={vi.fn()}
      onCloseThreadMenu={vi.fn()}
      onOpenSidebar={vi.fn()}
      {...handlers}
    />,
  );
  return handlers;
}

const thread: Thread = {
  id: "th1",
  title: "Cinema scout",
  starred: false,
  createdAt: "2026-10-01T00:00:00Z",
  updatedAt: "2026-10-22T12:39:00Z",
};

const messages: MessageWithActivityTrace[] = [
  {
    id: "u1",
    role: "user",
    content: "Find new films",
    createdAt: "2026-10-22T12:37:00Z",
  } as MessageWithActivityTrace,
  {
    id: "a1",
    role: "assistant",
    content: "Two new films.",
    createdAt: "2026-10-22T12:39:00Z",
  } as MessageWithActivityTrace,
];

test("a scheduled run's thread takes follow-ups and links to its schedule", () => {
  const handlers = panel({ ...thread, scheduleId: "s1" }, messages);
  expect(screen.getByRole("textbox")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: /Schedule/ }));
  expect(handlers.onOpenSchedule).toHaveBeenCalledWith("s1");
});

test("a plain thread shows no schedule link", () => {
  panel(thread, messages);
  expect(
    screen.queryByRole("button", { name: /^Schedule$/ }),
  ).not.toBeInTheDocument();
});
