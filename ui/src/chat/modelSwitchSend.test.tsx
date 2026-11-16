import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import App from "../App";
import { resetModelInfoForTest } from "../api/model";

// A message sent right after a model switch runs on the model just picked:
// the send carries the slot shown, so it never waits on (or races) the
// switch's own save.

const thread = {
  id: "t1",
  title: "Model compare",
  starred: false,
  chatSlot: "standard",
  createdAt: "2026-05-30T00:00:00Z",
  updatedAt: "2026-05-30T00:00:00Z",
};

const modelInfo = {
  model: "fast",
  displayName: "Fast Model",
  contextWindow: 1000,
  slots: [
    {
      key: "standard",
      model: "fast",
      displayName: "Fast Model",
      contextWindow: 1000,
    },
    {
      key: "deep",
      model: "deep",
      displayName: "Deep Model",
      contextWindow: 2000,
    },
  ],
};

// openSSE serves the chunks and then stays open: the turn is still running.
function openSSE(chunks: string[] = []) {
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

type Sent = { url: string; body: Record<string, unknown> };

// By default the switch's own save never answers: nothing may wait for it.
function stubFetch(
  sent: Sent[],
  patch: () => Promise<Response> = () => new Promise<Response>(() => {}),
  streamChunks: string[] = [],
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me")
        return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/model") return Response.json(modelInfo);
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30")
        return Response.json({ items: [thread], nextCursor: null });
      if (url === "/api/threads/t1" && init?.method === "PATCH") return patch();
      if (url === "/api/threads/t1")
        return Response.json({ thread, messages: [] });
      if (init?.method === "POST" && url.startsWith("/api/threads/t1/")) {
        sent.push({ url, body: JSON.parse(String(init.body)) });
        return url.endsWith("messages:stream")
          ? openSSE(streamChunks)
          : new Response(null, { status: 202 });
      }
      return Response.json({});
    }),
  );
}

async function typeAndSend(text: string, button = "Send message") {
  fireEvent.change(await screen.findByPlaceholderText(/message/i), {
    target: { value: text },
  });
  fireEvent.click(await screen.findByRole("button", { name: button }));
}

beforeEach(() => {
  window.history.replaceState({}, "", "/");
  window.localStorage.clear();
  resetModelInfoForTest();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("a send right after a model switch carries the slot just picked", async () => {
  const sent: Sent[] = [];
  stubFetch(sent);
  render(<App />);
  fireEvent.click(
    await screen.findByRole("button", { name: /^Model compare/ }),
  );
  fireEvent.click(await screen.findByRole("button", { name: /Fast Model/ }));
  fireEvent.click(screen.getByRole("menuitemradio", { name: /Deep Model/ }));

  await typeAndSend("Compare with the other model");
  await waitFor(() => expect(sent).toHaveLength(1));
  expect(sent[0].url).toBe("/api/threads/t1/messages:stream");
  expect(sent[0].body.chatSlot).toBe("deep");

  // While that turn runs, a further message is queued with the slot too.
  await typeAndSend("And one more thing", "Queue message");
  await waitFor(() => expect(sent).toHaveLength(2));
  expect(sent[1].url).toBe("/api/threads/t1/messages:queue");
  expect(sent[1].body.chatSlot).toBe("deep");
});

// The pick stays what answers: a failed save keeps it (the send saved it),
// and a thread event read before the switch landed does not flip it back.
test("a failed save or a stale thread event keeps the slot picked", async () => {
  const sent: Sent[] = [];
  const staleThread = { ...thread, title: "Renamed", chatSlot: "standard" };
  stubFetch(
    sent,
    async () => Response.json({ error: "nope" }, { status: 500 }),
    [`event: thread\ndata: ${JSON.stringify(staleThread)}\n\n`],
  );
  render(<App />);
  fireEvent.click(
    await screen.findByRole("button", { name: /^Model compare/ }),
  );
  fireEvent.click(await screen.findByRole("button", { name: /Fast Model/ }));
  fireEvent.click(screen.getByRole("menuitemradio", { name: /Deep Model/ }));

  await typeAndSend("Compare with the other model");
  await waitFor(() => expect(sent).toHaveLength(1));
  expect(sent[0].body.chatSlot).toBe("deep");
  // The thread event landed (its title shows) and the pick held.
  expect(
    await screen.findByRole("button", { name: /^Renamed/ }),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: /Deep Model/ }),
  ).toBeInTheDocument();
});
