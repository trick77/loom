import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

const markdownRenders = vi.hoisted(() => ({ count: 0 }));
vi.mock("react-markdown", () => ({
  default: (props: { children?: string }) => {
    markdownRenders.count += 1;
    return <div>{props.children}</div>;
  },
}));

// One render of a settled bubble's action row is one render of the bubble: the
// assistant row renders MessageMetrics, the user row formats its time. The
// sidebar is counted through its primary nav items.
const renders = vi.hoisted(() => ({ assistant: 0, user: 0, sidebar: 0 }));
vi.mock("../MessageMetrics", () => ({
  MessageMetrics: () => {
    renders.assistant += 1;
    return null;
  },
}));
vi.mock("../metrics", async () => {
  const actual =
    await vi.importActual<typeof import("../metrics")>("../metrics");
  return {
    ...actual,
    formatMessageTime: (createdAt: string) => {
      renders.user += 1;
      return actual.formatMessageTime(createdAt);
    },
  };
});
vi.mock("./SidebarItems", async () => {
  const actual =
    await vi.importActual<typeof import("./SidebarItems")>("./SidebarItems");
  return {
    ...actual,
    SidebarPrimaryItem: (
      props: Parameters<typeof actual.SidebarPrimaryItem>[0],
    ) => {
      renders.sidebar += 1;
      return actual.SidebarPrimaryItem(props);
    },
  };
});

import App from "../App";
import { ActivityTracePanel } from "./ActivityTracePanel";
import { AssistantProse } from "./messages";

afterEach(() => {
  vi.unstubAllGlobals();
});

// The transcript re-renders on every streamed token; a settled message's prose
// must not run the markdown pipeline again when nothing about it changed.
test("AssistantProse does not re-run markdown for identical props", () => {
  const view = render(<AssistantProse>hello **world**</AssistantProse>);
  expect(markdownRenders.count).toBe(1);

  view.rerender(<AssistantProse>hello **world**</AssistantProse>);
  expect(markdownRenders.count).toBe(1);

  view.rerender(<AssistantProse>hello **there**</AssistantProse>);
  expect(markdownRenders.count).toBe(2);
});

// A streaming trace re-renders on every reasoning delta; only the round that
// grew may run the markdown pipeline again.
test("an earlier reasoning round is not re-parsed while a later one streams", () => {
  const first = {
    id: "reasoning-1",
    type: "reasoning" as const,
    content: "First round.",
    status: "done" as const,
  };
  const second = (content: string) => ({
    id: "reasoning-2",
    type: "reasoning" as const,
    content,
    status: "running" as const,
  });
  const panel = (content: string) => (
    <ActivityTracePanel
      events={[first, second(content)]}
      active
      streaming
      initiallyExpanded
    />
  );
  const view = render(panel("Second"));
  const before = markdownRenders.count;

  view.rerender(panel("Second round"));

  expect(markdownRenders.count).toBe(before + 1);
});

// openExistingChat signs in, opens a thread holding one settled exchange and
// returns its composer.
async function openExistingChat() {
  const thread = {
    id: "t1",
    title: "Existing chat",
    starred: false,
    createdAt: "2026-05-30T00:00:00Z",
    updatedAt: "2026-05-30T00:00:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me")
        return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30")
        return Response.json({ items: [thread], nextCursor: null });
      if (url === "/api/threads/t1") {
        return Response.json({
          thread,
          messages: [
            {
              id: "m1",
              threadId: "t1",
              role: "user",
              content: "Make a short report",
              createdAt: "2026-05-30T00:00:00Z",
            },
            {
              id: "m2",
              threadId: "t1",
              role: "assistant",
              content: "A **short** report.",
              createdAt: "2026-05-30T00:00:01Z",
            },
          ],
        });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  const composer = await screen.findByPlaceholderText(/message/i);
  await screen.findByText("A **short** report.");
  return composer;
}

// Every keystroke re-renders the shell. The bubbles are memoized, so that must
// stop at the transcript: a callback rebuilt per shell render would defeat it.
test("typing in the composer does not re-render settled bubbles", async () => {
  const composer = await openExistingChat();
  const before = { ...renders };
  expect(before.assistant).toBeGreaterThan(0);
  expect(before.user).toBeGreaterThan(0);

  fireEvent.change(composer, { target: { value: "H" } });
  fireEvent.change(composer, { target: { value: "Hi" } });

  expect(composer).toHaveValue("Hi");
  expect(renders.assistant).toBe(before.assistant);
  expect(renders.user).toBe(before.user);
});

// The sidebar takes some forty props from the shell; one unstable handler among
// them re-renders every thread row on each keystroke and streamed token.
test("typing in the composer does not re-render the sidebar", async () => {
  const composer = await openExistingChat();
  const before = renders.sidebar;
  expect(before).toBeGreaterThan(0);

  fireEvent.change(composer, { target: { value: "H" } });
  fireEvent.change(composer, { target: { value: "Hi" } });

  expect(composer).toHaveValue("Hi");
  expect(renders.sidebar).toBe(before);
});
