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

// A run thread's sidebar clock goes the moment the server stores the user's
// reply, not only when the thread event arrives after the answer.

const thread = {
  id: "t1",
  title: "Cinema scout",
  starred: false,
  scheduleId: "s1",
  createdAt: "2026-05-30T00:00:00Z",
  updatedAt: "2026-05-30T00:00:00Z",
};

// openSSE serves the chunks and then stays open: the answer is still running.
function openSSE(chunks: string[]) {
  const encoder = new TextEncoder();
  const pending = [...chunks];
  return new Response(
    new ReadableStream<Uint8Array>({
      pull(controller) {
        const chunk = pending.shift();
        if (chunk === undefined) return new Promise<void>(() => {});
        controller.enqueue(encoder.encode(chunk));
      },
    }),
  );
}

beforeEach(() => {
  window.history.replaceState({}, "", "/");
  window.localStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("replying in a run thread drops its sidebar clock while the answer runs", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me")
        return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30")
        return Response.json({ items: [thread], nextCursor: null });
      if (url === "/api/threads/t1")
        return Response.json({ thread, messages: [] });
      if (url === "/api/threads/t1/messages:stream" && init?.method === "POST")
        return openSSE([
          'event: user_message\ndata: {"id":"m3","threadId":"t1","role":"user","content":"And next week?","createdAt":"2026-05-30T00:00:00Z"}\n\n',
          'event: assistant_delta\ndata: {"content":"Next"}\n\n',
        ]);
      return Response.json({});
    }),
  );

  render(<App />);
  const row = await screen.findByRole("button", { name: /^Cinema scout/ });
  expect(
    within(row).getByRole("img", { name: "Schedules" }),
  ).toBeInTheDocument();

  fireEvent.click(row);
  fireEvent.change(await screen.findByPlaceholderText(/message/i), {
    target: { value: "And next week?" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));

  await waitFor(() =>
    expect(
      within(screen.getByRole("button", { name: /^Cinema scout/ })).queryByRole(
        "img",
        { name: "Schedules" },
      ),
    ).not.toBeInTheDocument(),
  );
  expect(
    screen.getByRole("button", { name: "Stop response" }),
  ).toBeInTheDocument();
});
