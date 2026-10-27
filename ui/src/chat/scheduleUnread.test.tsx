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
