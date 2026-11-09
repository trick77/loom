import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import type { Thread } from "../api";
import { ThreadPanel } from "./ThreadPanel";
import type { MessageWithActivityTrace } from "./types";

function panel(
  thread: Thread,
  messages: MessageWithActivityTrace[],
  openAtAnswer = false,
) {
  const handlers = {
    onOpenSchedule: vi.fn(),
  };
  render(
    <ThreadPanel
      thread={thread}
      openAtAnswer={openAtAnswer}
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

test("a scheduled run's thread hides the schedule's prompt, not follow-ups", () => {
  panel({ ...thread, scheduleId: "s1" }, [
    ...messages,
    {
      id: "u2",
      role: "user",
      content: "When does the first one open?",
      createdAt: "2026-10-22T12:40:00Z",
    } as MessageWithActivityTrace,
  ]);
  expect(screen.queryByText("Find new films")).not.toBeInTheDocument();
  expect(screen.getByText("Two new films.")).toBeInTheDocument();
  expect(screen.getByText("When does the first one open?")).toBeInTheDocument();
});

// jsdom lays nothing out: the transcript gets a height, each message a top.
function fakeLayout(tops: Record<string, number>) {
  const scrollTop = new WeakMap<Element, number>();
  const restore = [
    vi
      .spyOn(HTMLElement.prototype, "scrollHeight", "get")
      .mockReturnValue(2000),
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(400),
    vi
      .spyOn(Element.prototype, "scrollTop", "get")
      .mockImplementation(function (this: Element) {
        return scrollTop.get(this) ?? 0;
      }),
    vi
      .spyOn(Element.prototype, "scrollTop", "set")
      .mockImplementation(function (this: Element, value: number) {
        scrollTop.set(this, value);
      }),
    vi
      .spyOn(Element.prototype, "getBoundingClientRect")
      .mockImplementation(function (this: Element) {
        const id = this.getAttribute("data-message-id");
        const top = id === null ? 100 : (tops[id] ?? 0);
        return { top } as DOMRect;
      }),
  ];
  return () => restore.forEach((spy) => spy.mockRestore());
}

function transcriptScrollTop() {
  return screen.getByRole("region", { name: "Conversation transcript" })
    .scrollTop;
}

test("unread news opens at the start of the answer", () => {
  const restore = fakeLayout({ a1: 100 });
  try {
    panel({ ...thread, scheduleId: "s1" }, messages, true);
    expect(transcriptScrollTop()).toBe(0);
    expect(
      screen.getByRole("button", { name: "Jump to latest message" }),
    ).toBeInTheDocument();
  } finally {
    restore();
  }
});

test("unread news in a thread of several runs opens at the latest answer", () => {
  const restore = fakeLayout({ a1: 100, a2: 900 });
  try {
    panel(
      { ...thread, scheduleId: "s1" },
      [
        ...messages,
        {
          id: "u2",
          role: "user",
          content: "Find new films",
          createdAt: "2026-10-23T12:37:00Z",
        } as MessageWithActivityTrace,
        {
          id: "a2",
          role: "assistant",
          content: "One more film.",
          createdAt: "2026-10-23T12:39:00Z",
        } as MessageWithActivityTrace,
      ],
      true,
    );
    expect(transcriptScrollTop()).toBe(800);
  } finally {
    restore();
  }
});

test("read news opens at the end", () => {
  const restore = fakeLayout({ a1: 100 });
  try {
    panel({ ...thread, scheduleId: "s1" }, messages);
    expect(transcriptScrollTop()).toBe(2000);
  } finally {
    restore();
  }
});

test("a plain thread shows no schedule link", () => {
  panel(thread, messages);
  expect(screen.getByText("Find new films")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /^Schedule$/ }),
  ).not.toBeInTheDocument();
});
