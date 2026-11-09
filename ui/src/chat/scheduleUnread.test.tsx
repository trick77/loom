import "@testing-library/jest-dom/vitest";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import App from "../App";
import { announce, THREAD_NEWS_CHANGED } from "../api";

// Opening a run thread with unread news reads it: the sidebar dot goes and
// the server marks the thread's notifications read.

const thread = {
  id: "t1",
  title: "Cinema scout",
  starred: false,
  scheduleId: "s1",
  scheduleUnread: true,
  createdAt: "2026-05-30T00:00:00Z",
  updatedAt: "2026-05-30T00:00:00Z",
};

beforeEach(() => {
  window.history.replaceState({}, "", "/");
  window.localStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("opening a run thread with unread news reads it", async () => {
  const fetchMock = vi.fn(
    async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me")
        return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30")
        return Response.json({ items: [thread], nextCursor: null });
      if (url === "/api/threads/t1")
        return Response.json({ thread, messages: [] });
      if (
        url === "/api/threads/t1/notifications/read" &&
        init?.method === "POST"
      )
        return new Response(null, { status: 204 });
      return Response.json({});
    },
  );
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  const row = await screen.findByRole("button", { name: /^Cinema scout/ });
  expect(
    within(row).getByRole("img", { name: "Unread news" }),
  ).toBeInTheDocument();

  fireEvent.click(row);

  await waitFor(() =>
    expect(
      within(screen.getByRole("button", { name: /^Cinema scout/ })).queryByRole(
        "img",
        { name: "Unread news" },
      ),
    ).not.toBeInTheDocument(),
  );
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads/t1/notifications/read",
    expect.objectContaining({ method: "POST" }),
  );
});

test("unread news opened from the bell opens at the answer", async () => {
  // The bell reads the news as it opens it: the thread loads already read.
  const read = { ...thread, scheduleUnread: false };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me")
        return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30")
        return Response.json({ items: [read], nextCursor: null });
      if (url === "/api/notifications/unread")
        return Response.json({ count: 1 });
      if (url === "/api/notifications")
        return Response.json([
          {
            id: "n1",
            kind: "schedule_run",
            scheduleId: "s1",
            threadId: "t1",
            messageId: "a1",
            title: "News run",
            body: "Two new films.",
            createdAt: "2026-05-30T00:00:00Z",
            readAt: null,
          },
        ]);
      if (url === "/api/threads/t1")
        return Response.json({
          thread: read,
          messages: [
            {
              id: "u1",
              role: "user",
              content: "find films",
              createdAt: "2026-05-30T00:00:00Z",
            },
            {
              id: "a1",
              role: "assistant",
              content: "Two new films.",
              createdAt: "2026-05-30T00:01:00Z",
            },
          ],
        });
      return new Response(null, { status: 204 });
    }),
  );
  const spies = [
    vi
      .spyOn(HTMLElement.prototype, "scrollHeight", "get")
      .mockReturnValue(2000),
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(400),
    // jsdom lays nothing out: the transcript stays at its top unless the
    // panel scrolls it to the end (which then shows no jump button).
    vi.spyOn(Element.prototype, "scrollTop", "get").mockReturnValue(0),
  ];
  try {
    render(<App />);
    fireEvent.click(
      await screen.findByRole("button", { name: /Notifications, some unread/ }),
    );
    fireEvent.click(await screen.findByText("News run"));
    expect(
      await screen.findByRole("button", { name: "Jump to latest message" }),
    ).toBeInTheDocument();
  } finally {
    spies.forEach((spy) => spy.mockRestore());
  }
});

test("news read in the bell reloads the sidebar's dots", async () => {
  let listed = thread;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me")
        return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30")
        return Response.json({ items: [listed], nextCursor: null });
      return Response.json({});
    }),
  );

  render(<App />);
  const row = await screen.findByRole("button", { name: /^Cinema scout/ });
  expect(
    within(row).getByRole("img", { name: "Unread news" }),
  ).toBeInTheDocument();

  listed = { ...thread, scheduleUnread: false };
  announce(THREAD_NEWS_CHANGED);

  await waitFor(() =>
    expect(
      within(screen.getByRole("button", { name: /^Cinema scout/ })).queryByRole(
        "img",
        { name: "Unread news" },
      ),
    ).not.toBeInTheDocument(),
  );
});
