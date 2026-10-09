import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import App from "../App";

// A phone that freezes the tab mid-answer drops the stream; the server keeps
// the turn running. These drive the whole shell through that: the answer must
// come back without an error and without the question returning to the
// composer for a resend.

const thread = {
  id: "t1",
  title: "Existing chat",
  starred: false,
  createdAt: "2026-05-30T00:00:00Z",
  updatedAt: "2026-05-30T00:00:00Z",
};

const userMessage =
  'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n';

function answerEvents(content: string) {
  return [
    `event: assistant_delta\ndata: {"content":"${content}"}\n\n`,
    `event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"${content}","createdAt":"2026-05-30T00:00:01Z"}\n\n`,
    "event: done\ndata: {}\n\n",
  ];
}

// sse serves chunks one per read; dropAtEnd then fails the read the way a
// browser does when the network goes away (Safari: TypeError "Load failed").
function sse(chunks: string[], dropAtEnd = false) {
  const encoder = new TextEncoder();
  const pending = [...chunks];
  return new Response(
    new ReadableStream<Uint8Array>({
      pull(controller) {
        const chunk = pending.shift();
        if (chunk !== undefined) controller.enqueue(encoder.encode(chunk));
        else if (dropAtEnd) controller.error(new TypeError("Load failed"));
        else controller.close();
      },
    }),
  );
}

function shellFetch(routes: {
  thread: () => Response;
  stream?: (sendId: string) => Response;
  attach: () => Response;
  stop?: (url: string) => Response | Promise<Response>;
}) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me")
      return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30")
      return Response.json({ items: [thread], nextCursor: null });
    if (url === "/api/threads/t1") return routes.thread();
    if (
      url === "/api/threads/t1/messages:stream" &&
      init?.method === "POST" &&
      routes.stream !== undefined
    ) {
      const body = JSON.parse(String(init.body)) as {
        clientMessageId?: string;
      };
      return routes.stream(body.clientMessageId ?? "");
    }
    if (url.startsWith("/api/threads/t1/messages:stop") && routes.stop)
      return routes.stop(url);
    if (url === "/api/threads/t1/messages:attach") return routes.attach();
    throw new Error(`unexpected fetch ${url}`);
  });
}

beforeEach(() => {
  window.history.replaceState({}, "", "/");
  window.localStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

async function sendHi() {
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), {
    target: { value: "Hi" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));
}

test("a stream dropped mid-answer reattaches and finishes the answer", async () => {
  const fetchMock = shellFetch({
    thread: () => Response.json({ thread, messages: [] }),
    stream: () =>
      sse(
        [userMessage, 'event: assistant_delta\ndata: {"content":"Hel"}\n\n'],
        true,
      ),
    attach: () => sse([userMessage, ...answerEvents("Hello there")]),
  });
  vi.stubGlobal("fetch", fetchMock);

  await sendHi();

  expect(await screen.findByText("Hello there")).toBeInTheDocument();
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Stop response" }),
    ).not.toBeInTheDocument(),
  );
  expect(screen.queryByText(/failed to send/i)).not.toBeInTheDocument();
  expect(screen.queryByText(/connection dropped/i)).not.toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toHaveValue("");
  expect(screen.getAllByText("Hi")).toHaveLength(1);
});

// The server already has the question once it confirmed it: putting it back
// in the composer would invite a duplicate send.
test("a reattach that fails keeps the confirmed question out of the composer", async () => {
  const fetchMock = shellFetch({
    thread: () => Response.json({ thread, messages: [] }),
    stream: () => sse([userMessage], true),
    attach: () => Response.json({ error: "boom" }, { status: 500 }),
  });
  vi.stubGlobal("fetch", fetchMock);

  await sendHi();

  expect(await screen.findByText("boom")).toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toHaveValue("");
});

test("a turn that finished while the stream was down is loaded from the thread", async () => {
  let threadLoads = 0;
  const fetchMock = shellFetch({
    thread: () => {
      threadLoads += 1;
      return Response.json({
        thread,
        messages:
          threadLoads === 1
            ? []
            : [
                {
                  id: "m1",
                  threadId: "t1",
                  role: "user",
                  content: "Hi",
                  createdAt: "2026-05-30T00:00:00Z",
                },
                {
                  id: "m2",
                  threadId: "t1",
                  role: "assistant",
                  content: "Saved answer",
                  createdAt: "2026-05-30T00:00:01Z",
                },
              ],
      });
    },
    stream: () => sse([userMessage], true),
    attach: () => new Response(null, { status: 204 }),
  });
  vi.stubGlobal("fetch", fetchMock);

  await sendHi();

  expect(await screen.findByText("Saved answer")).toBeInTheDocument();
  expect(screen.queryByText(/failed to send/i)).not.toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toHaveValue("");
});

test("opening a thread whose answer is still being written follows it live", async () => {
  const fetchMock = shellFetch({
    thread: () =>
      Response.json({
        thread,
        streaming: true,
        messages: [
          {
            id: "m1",
            threadId: "t1",
            role: "user",
            content: "Hi",
            createdAt: "2026-05-30T00:00:00Z",
          },
        ],
      }),
    attach: () => sse([userMessage, ...answerEvents("Still coming")]),
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  expect(await screen.findByText("Still coming")).toBeInTheDocument();
  // The replayed user message folds into the loaded one.
  expect(screen.getAllByText("Hi")).toHaveLength(1);
});

// The connection can drop after the server stored the question but before the
// first event arrived. The question is on the server and the turn runs there:
// that is no failed send.
test("a drop before the first event follows the turn the server started", async () => {
  let threadLoads = 0;
  let sentId = "";
  const fetchMock = shellFetch({
    thread: () => {
      threadLoads += 1;
      return Response.json({
        thread,
        streaming: threadLoads > 1,
        messages:
          threadLoads === 1
            ? []
            : [
                {
                  id: "m1",
                  threadId: "t1",
                  role: "user",
                  content: "Hi",
                  clientMessageId: sentId,
                  createdAt: "2026-05-30T00:00:00Z",
                },
              ],
      });
    },
    stream: (sendId) => {
      sentId = sendId;
      throw new TypeError("Load failed");
    },
    attach: () => sse([userMessage, ...answerEvents("Made it")]),
  });
  vi.stubGlobal("fetch", fetchMock);

  await sendHi();

  expect(await screen.findByText("Made it")).toBeInTheDocument();
  expect(screen.queryByText(/failed to send/i)).not.toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toHaveValue("");
  expect(screen.getAllByText("Hi")).toHaveLength(1);
});

test("a send that never reached the server still fails as before", async () => {
  const fetchMock = shellFetch({
    thread: () => Response.json({ thread, messages: [] }),
    stream: () => {
      throw new TypeError("Load failed");
    },
    attach: () => new Response(null, { status: 204 }),
  });
  vi.stubGlobal("fetch", fetchMock);

  await sendHi();

  expect(await screen.findByText(/failed to send/i)).toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toHaveValue("Hi");
});

// The same text sent earlier is not this send: only the send id counts.
test("an older identical question does not pass for the dropped send", async () => {
  const older = {
    id: "m0",
    threadId: "t1",
    role: "user",
    content: "Hi",
    clientMessageId: "send-older",
    createdAt: "2026-05-29T00:00:00Z",
  };
  const fetchMock = shellFetch({
    thread: () => Response.json({ thread, messages: [older] }),
    stream: () => {
      throw new TypeError("Load failed");
    },
    attach: () => new Response(null, { status: 204 }),
  });
  vi.stubGlobal("fetch", fetchMock);

  await sendHi();

  expect(await screen.findByText(/failed to send/i)).toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toHaveValue("Hi");
});

// A stop names the send it belongs to, and one that fails leaves the answer
// running on screen: dropping the fetch no longer stops the server.
test("a stop that fails keeps the answer running and says so", async () => {
  let release: (() => void) | null = null;
  const stopURLs: string[] = [];
  let sentId = "";
  const fetchMock = shellFetch({
    thread: () => Response.json({ thread, messages: [] }),
    stream: (sendId) => {
      sentId = sendId;
      const encoder = new TextEncoder();
      return new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(encoder.encode(userMessage));
            release = () => {
              for (const event of answerEvents("Kept going"))
                controller.enqueue(encoder.encode(event));
              controller.close();
            };
          },
        }),
      );
    },
    attach: () => new Response(null, { status: 204 }),
    stop: (url) => {
      stopURLs.push(url);
      return new Response("", { status: 503 });
    },
  });
  vi.stubGlobal("fetch", fetchMock);

  await sendHi();
  fireEvent.click(await screen.findByRole("button", { name: "Stop response" }));

  expect(await screen.findByText(/failed to stop/i)).toBeInTheDocument();
  expect(stopURLs[0]).toContain(`sendId=${sentId}`);
  expect(sentId).not.toBe("");
  expect(screen.getByRole("button", { name: "Stop response" })).toBeVisible();
  release!();
  expect(await screen.findByText("Kept going")).toBeInTheDocument();
});
