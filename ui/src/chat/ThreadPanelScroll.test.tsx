import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import type { Thread } from "../api";
import { ThreadPanel } from "./ThreadPanel";
import type { MessageWithActivityTrace } from "./types";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const thread: Thread = {
  id: "th1",
  title: "Long thread",
  starred: false,
  createdAt: "2026-10-01T00:00:00Z",
  updatedAt: "2026-10-22T12:39:00Z",
};

const messages: MessageWithActivityTrace[] = [
  {
    id: "u1",
    role: "user",
    content: "Tell me a lot",
    createdAt: "2026-10-22T12:37:00Z",
  } as MessageWithActivityTrace,
];

// jsdom lays nothing out: the transcript is 400px tall and holds 2000px plus
// whatever the composer dock grows by; observers fire on demand.
function fakeLayout() {
  const layout = { dockGrowth: 0 };
  const scrollTop = new WeakMap<Element, number>();
  const observers: { target: Element; fire: () => void }[] = [];
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private callback: () => void) {}
      observe(target: Element) {
        observers.push({ target, fire: () => this.callback() });
      }
      unobserve() {}
      disconnect() {}
    },
  );
  const isDock = (el: Element) =>
    el.getAttribute("aria-label") === "Message composer dock";
  vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockImplementation(
    function (this: HTMLElement) {
      return this.getAttribute("role") === "region"
        ? 2000 + layout.dockGrowth
        : 0;
    },
  );
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(400);
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(
    function (this: HTMLElement) {
      return isDock(this) ? 120 + layout.dockGrowth : 0;
    },
  );
  vi.spyOn(Element.prototype, "scrollTop", "get").mockImplementation(function (
    this: Element,
  ) {
    return scrollTop.get(this) ?? 0;
  });
  vi.spyOn(Element.prototype, "scrollTop", "set").mockImplementation(function (
    this: Element,
    value: number,
  ) {
    const max = 2000 + layout.dockGrowth - 400;
    scrollTop.set(this, Math.max(0, Math.min(value, max)));
  });
  const growDock = (by: number) => {
    layout.dockGrowth += by;
    observers
      .filter((observer) => isDock(observer.target))
      .forEach((observer) => observer.fire());
  };
  return { growDock };
}

function renderPanel() {
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
      isSending
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
    />,
  );
  return screen.getByRole("region", { name: "Conversation transcript" });
}

test("a composer growing by a line keeps the transcript on its bottom", () => {
  const { growDock } = fakeLayout();
  const transcript = renderPanel();
  expect(transcript.scrollTop).toBe(1600);
  growDock(21);
  expect(transcript.scrollTop).toBe(1621);
});

test("a composer growing leaves a reader who scrolled up where they are", () => {
  const { growDock } = fakeLayout();
  const transcript = renderPanel();
  fireEvent.wheel(transcript, { deltaY: -100 });
  transcript.scrollTop = 500;
  growDock(21);
  expect(transcript.scrollTop).toBe(500);
});
