import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, test, vi } from "vitest";
import App from "./App";

beforeEach(() => {
  window.history.replaceState({}, "", "/");
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("renders signed-out screen when /api/me returns 401", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("", { status: 401 })));

  render(<App />);

  expect(await screen.findByRole("link", { name: /sign in/i })).toHaveAttribute(
    "href",
    "/api/auth/login",
  );
  expect(screen.getByAltText("Spark")).toBeInTheDocument();
});

test("renders authenticated shell for signed-in users", async () => {
  vi.stubGlobal("fetch", basicSignedInFetch());

  render(<App />);

  expect(await screen.findByRole("button", { name: /new chat/i })).toBeInTheDocument();
  expect(await screen.findByText(greetingPattern("Jan"))).toBeInTheDocument();
  expect(screen.getByText("Jan")).toBeInTheDocument();
  expect(window.location.pathname).toBe("/new");
});

test("bounds the active chat title in the top header", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") {
        return Response.json({ id: "u1", username: "jan", role: "user", displayName: "Jan" });
      }
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") {
        return Response.json([
          {
            ...threadFixture(),
            title: "Albert Einstein The legendary physicist who revolutionized modern physics",
          },
        ]);
      }
      if (url === "/api/threads/t1") {
        return Response.json({
          thread: {
            ...threadFixture(),
            title: "Albert Einstein The legendary physicist who revolutionized modern physics",
          },
          messages: [],
        });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  fireEvent.click(
    await screen.findByRole("button", {
      name: "Albert Einstein The legendary physicist who revolutionized modern physics",
    }),
  );

  const heading = await screen.findByRole("heading", {
    name: /Albert Einstein The legendary physicist/,
  });
  expect(heading).toHaveClass("truncate");
  expect(heading).toHaveClass("max-w-[28ch]");
  expect(heading).toHaveClass("sm:max-w-[48ch]");
});

test("shows a visible send control in the new chat composer", async () => {
  vi.stubGlobal("fetch", basicSignedInFetch());

  render(<App />);

  await screen.findByPlaceholderText("How can I help you today?");
  const sendButton = screen.getByRole("button", { name: /send message/i });

  expect(sendButton).not.toHaveClass("sr-only");
  expect(sendButton).toBeDisabled();

  fireEvent.change(screen.getByPlaceholderText("How can I help you today?"), {
    target: { value: "Hello" },
  });

  expect(sendButton).toBeEnabled();
});

test("renders admin user list for admin users", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") {
        return Response.json({ id: "u1", username: "jan", role: "admin", displayName: "Jan" });
      }
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") return Response.json([]);
      if (url === "/api/admin/users") {
        return Response.json([{ id: "u2", username: "sam", role: "user", displayName: "Sam" }]);
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  fireEvent.click(await screen.findByRole("button", { name: /admin/i }));

  expect(await screen.findByText("Sam")).toBeInTheDocument();
});

test("loads projects and recent threads after sign in", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") {
        return Response.json({ id: "u1", username: "jan", role: "user", displayName: "Jan" });
      }
      if (url === "/api/projects") {
        return Response.json([
          {
            id: "p1",
            name: "School",
            description: "",
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
        ]);
      }
      if (url === "/api/threads?limit=30") {
        return Response.json([
          {
            id: "t1",
            title: "Algebra",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
        ]);
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  expect(await screen.findByText("School")).toBeInTheDocument();
  expect(screen.getByText("Algebra")).toBeInTheDocument();
});

test("shows chat data load errors", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return new Response("", { status: 500 });
      if (url === "/api/threads?limit=30") return Response.json([]);
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  expect(await screen.findByText("Chat data failed to load.")).toBeInTheDocument();
});

test("creates a project from the sidebar", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects" && init?.method === undefined) return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([]);
    if (url === "/api/projects" && init?.method === "POST") {
      return new Response(
        JSON.stringify({
          id: "p1",
          name: "School",
          description: "",
          createdAt: "2026-05-30T00:00:00Z",
          updatedAt: "2026-05-30T00:00:00Z",
        }),
        { status: 201 },
      );
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: /new project/i }));
  const projectNameInput = screen.getByPlaceholderText(/project name/i);
  fireEvent.change(projectNameInput, { target: { value: "School" } });
  await waitFor(() => expect(screen.getByRole("button", { name: "Create" })).toBeEnabled());
  fireEvent.submit(projectNameInput.closest("form")!);

  expect(await screen.findByText("School")).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/projects",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ name: "School" }),
    }),
  );
});

test("new chat navigation does not create a thread or sidebar entry", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([{ ...threadFixture(), id: "existing", title: "Existing chat" }]);
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  const button = await screen.findByRole("button", { name: /new chat/i });
  fireEvent.click(button);
  fireEvent.click(button);

  expect(await screen.findByText(greetingPattern("jan"))).toBeInTheDocument();
  expect(window.location.pathname).toBe("/new");
  expect(await screen.findByRole("button", { name: "Existing chat" })).toBeInTheDocument();
  expect(
    fetchMock.mock.calls.filter(([url, init]) => String(url) === "/api/threads" && init?.method === "POST"),
  ).toHaveLength(0);
});

test("inserts the titled sidebar chat before rendering the first new chat response", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
    },
  });
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([]);
    if (url === "/api/threads" && init?.method === "POST") {
      return new Response(
        JSON.stringify({
          id: "t1",
          title: "New chat",
          starred: false,
          createdAt: "2026-05-30T00:00:00Z",
          updatedAt: "2026-05-30T00:00:00Z",
        }),
        { status: 201 },
      );
    }
    if (url === "/api/threads/t1/messages:stream" && init?.method === "POST") return new Response(stream, { status: 200 });
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.change(await screen.findByPlaceholderText("How can I help you today?"), {
    target: { value: "It is hot" },
  });
  fireEvent.click(screen.getByRole("button", { name: /send message/i }));

  await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads/t1/messages:stream",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ content: "It is hot" }),
    }),
  ));

  const encoder = new TextEncoder();
  streamController.current?.enqueue(
    encoder.encode(
      'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"It is hot","createdAt":"2026-05-30T00:00:00Z"}\n\n',
    ),
  );
  streamController.current?.enqueue(
    encoder.encode(
      'event: thread\ndata: {"id":"t1","title":"Weather comfort","starred":false,"createdAt":"2026-05-30T00:00:00Z","updatedAt":"2026-05-30T00:00:02Z","lastMessageAt":"2026-05-30T00:00:01Z"}\n\n',
    ),
  );

  expect(await screen.findByText("It is hot")).toBeInTheDocument();
  expect(window.location.pathname).toBe("/chat/t1");
  expect(screen.queryByRole("button", { name: "New chat" })).not.toBeInTheDocument();
  expect(await screen.findByRole("button", { name: "Weather comfort" })).toBeInTheDocument();
  expect(screen.queryByText("Drink water.")).not.toBeInTheDocument();

  streamController.current?.enqueue(
    encoder.encode(
      'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Drink water.","createdAt":"2026-05-30T00:00:01Z"}\n\n',
    ),
  );

  expect(await screen.findByText("Drink water.")).toBeInTheDocument();

  streamController.current?.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
  streamController.current?.close();
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads",
    expect.objectContaining({ method: "POST" }),
  );
});

test("stars and unstars the active chat", async () => {
  let starred = false;
  const thread = () => ({
    id: "t1",
    title: "Existing chat",
    starred,
    createdAt: "2026-05-30T00:00:00Z",
    updatedAt: "2026-05-30T00:00:00Z",
  });
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([thread()]);
    if (url === "/api/threads/t1") return Response.json({ thread: thread(), messages: [] });
    if (url === "/api/threads/t1/star" && init?.method === "POST") {
      starred = true;
      return Response.json(thread());
    }
    if (url === "/api/threads/t1/unstar" && init?.method === "POST") {
      starred = false;
      return Response.json(thread());
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  fireEvent.click(await screen.findByRole("button", { name: "Star chat" }));
  expect(await screen.findByRole("button", { name: "Unstar chat" })).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1/star", { method: "POST" });

  fireEvent.click(screen.getByRole("button", { name: "Unstar chat" }));
  expect(await screen.findByRole("button", { name: "Star chat" })).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1/unstar", { method: "POST" });
});

test("starting chat exits the admin panel", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "admin" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") return Response.json([]);
      if (url === "/api/admin/users") {
        return Response.json([{ id: "u2", username: "sam", role: "user", displayName: "Sam" }]);
      }
      if (url === "/api/threads" && init?.method === "POST") {
        return new Response(
          JSON.stringify({
            id: "t1",
            title: "New chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          }),
          { status: 201 },
        );
      }
      if (url === "/api/threads/t1") {
        return Response.json({
          thread: {
            id: "t1",
            title: "New chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
          messages: [],
        });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: /admin/i }));
  expect(await screen.findByText("Sam")).toBeInTheDocument();

  fireEvent.click(await screen.findByRole("button", { name: /new chat/i }));

  expect(await screen.findByText(greetingPattern("jan"))).toBeInTheDocument();
  expect(screen.getByPlaceholderText("How can I help you today?")).toBeInTheDocument();
});

test("renders streamed assistant response", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(
        encoder.encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
      controller.enqueue(encoder.encode('event: assistant_delta\ndata: {"content":"Hel"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_delta\ndata: {"content":"lo"}\n\n'));
      controller.enqueue(
        encoder.encode(
          'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Hello","createdAt":"2026-05-30T00:00:01Z"}\n\n',
        ),
      );
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") {
        return Response.json([
          {
            id: "t1",
            title: "Existing chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
        ]);
      }
      if (url === "/api/threads/t1") {
        return Response.json({
          thread: {
            id: "t1",
            title: "Existing chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
          messages: [],
        });
      }
      if (url === "/api/threads/t1/messages:stream" && init?.method === "POST") {
        return new Response(stream, { status: 200 });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("Hello")).toBeInTheDocument();
});

test("renders streamed reasoning in a collapsed thinking panel", async () => {
  vi.stubGlobal(
    "fetch",
    mcpStreamFetch(
      'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n' +
        'event: assistant_reasoning_delta\ndata: {"content":"I checked the source first."}\n\n' +
        'event: assistant_delta\ndata: {"content":"Answer."}\n\n' +
        'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Answer.","reasoningContent":"I checked the source first.","createdAt":"2026-05-30T00:00:01Z"}\n\n' +
        "event: done\ndata: {}\n\n",
    ),
  );

  await sendMessageInExistingChat();

  const toggle = await screen.findByRole("button", { name: /show thinking/i });
  expect(toggle).toBeInTheDocument();
  expect(screen.queryByText("I checked the source first.")).not.toBeInTheDocument();

  fireEvent.click(toggle);

  expect(await screen.findByText("I checked the source first.")).toBeInTheDocument();
  expect(screen.getByText("Answer.")).toBeInTheDocument();
});

test("shows a thinking indicator while waiting for the first assistant output", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
      controller.enqueue(
        new TextEncoder().encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByRole("status", { name: /spark is thinking/i })).toBeInTheDocument();

  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Hel"}\n\n'));

  expect(await screen.findByText("Hel")).toBeInTheDocument();
  await waitFor(() => expect(screen.queryByRole("status", { name: /spark is thinking/i })).not.toBeInTheDocument());
});

test("hides the thinking indicator when the stream fails", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
      controller.enqueue(
        new TextEncoder().encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByRole("status", { name: /spark is thinking/i })).toBeInTheDocument();

  streamController.current?.enqueue(new TextEncoder().encode('event: error\ndata: {"error":"llm is not configured"}\n\n'));
  streamController.current?.close();

  expect(await screen.findByText("llm is not configured")).toBeInTheDocument();
  expect(screen.queryByRole("status", { name: /spark is thinking/i })).not.toBeInTheDocument();
});

test("keeps the transcript pinned while an assistant response streams at the bottom", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
      controller.enqueue(
        new TextEncoder().encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  await screen.findByText("Hi");
  const transcript = screen.getByRole("region", { name: /conversation transcript/i });
  let scrollHeight = 1000;
  let scrollTop = 900;
  Object.defineProperties(transcript, {
    clientHeight: { configurable: true, value: 100 },
    scrollHeight: {
      configurable: true,
      get: () => scrollHeight,
    },
    scrollTop: {
      configurable: true,
      get: () => scrollTop,
      set: (value: number) => {
        scrollTop = value;
      },
    },
  });

  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Hel"}\n\n'));
  await screen.findByText("Hel");
  await waitFor(() => expect(scrollTop).toBe(1000));

  scrollHeight = 1200;
  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"lo"}\n\n'));
  await screen.findByText("Hello");
  await waitFor(() => expect(scrollTop).toBe(1200));
});

test("shows a bottom jump control when the transcript is scrolled above the latest message", async () => {
  vi.stubGlobal("fetch", chatThreadFetch(null, [{ id: "m1", role: "assistant", content: "Earlier answer" }]));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const transcript = await screen.findByRole("region", { name: /conversation transcript/i });
  let scrollTop = 100;
  Object.defineProperties(transcript, {
    clientHeight: { configurable: true, value: 300 },
    scrollHeight: { configurable: true, value: 900 },
    scrollTop: {
      configurable: true,
      get: () => scrollTop,
      set: (value: number) => {
        scrollTop = value;
      },
    },
  });

  fireEvent.scroll(transcript);

  const jumpButton = await screen.findByRole("button", { name: /jump to latest message/i });
  fireEvent.click(jumpButton);

  expect(scrollTop).toBe(900);
  await waitFor(() => expect(screen.queryByRole("button", { name: /jump to latest message/i })).not.toBeInTheDocument());
});

test("scrolls to the latest message when sending from above the bottom", async () => {
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(
        new TextEncoder().encode(
          'event: user_message\ndata: {"id":"m2","threadId":"t1","role":"user","content":"Continue","createdAt":"2026-05-30T00:00:01Z"}\n\n' +
            'event: assistant_message\ndata: {"id":"m3","threadId":"t1","role":"assistant","content":"Immediate answer","createdAt":"2026-05-30T00:00:02Z"}\n\n',
        ),
      );
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream, [{ id: "m1", role: "assistant", content: "Earlier answer" }]));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const transcript = await screen.findByRole("region", { name: /conversation transcript/i });
  let scrollTop = 100;
  Object.defineProperties(transcript, {
    clientHeight: { configurable: true, value: 300 },
    scrollHeight: { configurable: true, value: 900 },
    scrollTop: {
      configurable: true,
      get: () => scrollTop,
      set: (value: number) => {
        scrollTop = value;
      },
    },
  });
  fireEvent.scroll(transcript);

  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Continue" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  await screen.findByText("Continue");
  await screen.findByText("Immediate answer");
  await waitFor(() => expect(scrollTop).toBe(900));
});

test("masks transcript content behind the overlaid composer dock", async () => {
  vi.stubGlobal("fetch", chatThreadFetch(null, [{ id: "m1", role: "assistant", content: "Earlier answer" }]));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const dock = await screen.findByLabelText("Message composer dock");
  expect(dock).toHaveClass("bg-bg");
});

function mcpStreamFetch(streamBody: string) {
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(new TextEncoder().encode(streamBody));
      controller.close();
    },
  });
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") {
      return Response.json([
        { id: "t1", title: "Existing chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" },
      ]);
    }
    if (url === "/api/threads/t1") {
      return Response.json({
        thread: { id: "t1", title: "Existing chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" },
        messages: [],
      });
    }
    if (url === "/api/threads/t1/messages:stream" && init?.method === "POST") {
      return new Response(stream, { status: 200 });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
}

function chatThreadFetch(
  stream: ReadableStream<Uint8Array> | null,
  messages: Array<{ id: string; role: "assistant" | "user"; content: string }> = [],
) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") {
      return Response.json([
        { id: "t1", title: "Existing chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" },
      ]);
    }
    if (url === "/api/threads/t1") {
      return Response.json({
        thread: { id: "t1", title: "Existing chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" },
        messages: messages.map((message, index) => ({
          ...message,
          threadId: "t1",
          createdAt: `2026-05-30T00:00:0${index}Z`,
        })),
      });
    }
    if (url === "/api/threads/t1/messages:stream" && init?.method === "POST" && stream !== null) {
      return new Response(stream, { status: 200 });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
}

function persistedMarkdownChatFetch() {
  const retryStreamBody =
    'event: assistant_message\ndata: {"id":"m4","threadId":"t1","role":"assistant","content":"Retried","createdAt":"2026-05-30T00:00:03Z"}\n\n' +
    "event: done\ndata: {}\n\n";
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") {
      return Response.json([
        { id: "t1", title: "Existing chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" },
      ]);
    }
    if (url === "/api/threads/t1") {
      return Response.json({
        thread: { id: "t1", title: "Existing chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" },
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
            content: "# Overview\n\nA **short** report.",
            createdAt: "2026-05-30T00:00:01Z",
          },
        ],
      });
    }
    if (url === "/api/threads/t1/messages:stream" && init?.method === "POST") {
      return new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(new TextEncoder().encode(retryStreamBody));
            controller.close();
          },
        }),
        { status: 200 },
      );
    }
    throw new Error(`unexpected fetch ${url}`);
  });
}

const assistantMessageEvent =
  'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Hello","createdAt":"2026-05-30T00:00:01Z"}\n\n';

function assistantEventForContent(content: string) {
  return `event: assistant_message\ndata: ${JSON.stringify({
    id: "m2",
    threadId: "t1",
    role: "assistant",
    content,
    createdAt: "2026-05-30T00:00:01Z",
  })}\n\n`;
}

async function sendMessageInExistingChat() {
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));
}

test("shows a red MCP indicator when not all servers are active", async () => {
  vi.stubGlobal(
    "fetch",
    mcpStreamFetch(
      assistantMessageEvent +
        'event: mcp_status\ndata: {"active":2,"configured":3}\n\n' +
        "event: done\ndata: {}\n\n",
    ),
  );

  await sendMessageInExistingChat();

  const indicator = await screen.findByTitle("2 of 3 MCP servers active");
  expect(indicator).toHaveTextContent("2");
  expect(indicator.querySelector(".border-danger")).not.toBeNull();
});

test("shows a green MCP indicator when all servers are active", async () => {
  vi.stubGlobal(
    "fetch",
    mcpStreamFetch(
      assistantMessageEvent +
        'event: mcp_status\ndata: {"active":3,"configured":3}\n\n' +
        "event: done\ndata: {}\n\n",
    ),
  );

  await sendMessageInExistingChat();

  const indicator = await screen.findByTitle("3 of 3 MCP servers active");
  expect(indicator).toHaveTextContent("3");
  expect(indicator.querySelector(".border-success")).not.toBeNull();
});

test("loads MCP status when the chat shell first renders", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") return Response.json([]);
      if (url === "/api/mcp/status") return Response.json({ active: 1, configured: 2 });
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  const indicator = await screen.findByTitle("1 of 2 MCP servers active");
  expect(indicator).toHaveTextContent("1");
  expect(indicator.querySelector(".border-danger")).not.toBeNull();
});

test("hides the MCP indicator when no mcp_status event arrives", async () => {
  vi.stubGlobal("fetch", mcpStreamFetch(assistantMessageEvent + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(await screen.findByText("Hello")).toBeInTheDocument();
  expect(screen.queryByTitle(/MCP servers active/)).toBeNull();
});

test("renders assistant markdown without rendering raw HTML", async () => {
  const content = "# Overview\\n\\nA **classic** film.\\n\\n- AI\\n- Control\\n\\n<div>raw html</div>";
  const event =
    `event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"${content}","createdAt":"2026-05-30T00:00:01Z"}\n\n` +
    "event: done\ndata: {}\n\n";
  vi.stubGlobal("fetch", mcpStreamFetch(event));

  await sendMessageInExistingChat();

  expect(await screen.findByRole("heading", { name: "Overview" })).toBeInTheDocument();
  expect(screen.getByText("classic").tagName).toBe("STRONG");
  expect(screen.getByRole("list")).toBeInTheDocument();
  expect(screen.getByText(/<div>raw html<\/div>/)).toBeInTheDocument();
  expect(document.querySelector(".spark-markdown div")).toBeNull();
  expect(screen.getByRole("button", { name: "Copy response" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Download response" })).not.toBeInTheDocument();
});

test("shows copy and retry actions for saved markdown assistant responses", async () => {
  const fetchMock = persistedMarkdownChatFetch();
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  expect(await screen.findByRole("heading", { name: "Overview" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Copy response" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Retry response" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Download response" })).not.toBeInTheDocument();
});

test("aligns chat messages and composer to the same readable rail", async () => {
  vi.stubGlobal("fetch", persistedMarkdownChatFetch());

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const transcript = await screen.findByRole("region", { name: "Conversation transcript" });
  const composerDock = screen.getByLabelText("Message composer dock");

  expect(transcript.querySelector(".spark-chat-rail")).toBeInTheDocument();
  expect(composerDock.querySelector(".spark-chat-rail")).toBeInTheDocument();
  expect(transcript.querySelector(".spark-user-message")).toHaveClass("ml-auto");
  expect(transcript.querySelector(".spark-assistant-message")).toBeInTheDocument();
});

test("anchors the chat send button inside the composer action area", async () => {
  vi.stubGlobal("fetch", persistedMarkdownChatFetch());

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const sendButton = screen.getByRole("button", { name: "Send message" });

  expect(sendButton.closest("form")).toHaveClass("spark-composer");
  expect(sendButton).toHaveClass("spark-composer-send");
});

test("copying a markdown assistant response writes rendered plain text", async () => {
  const writeText = vi.fn();
  vi.stubGlobal("navigator", { clipboard: { writeText } });
  vi.stubGlobal("fetch", persistedMarkdownChatFetch());

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Copy response" }));

  expect(writeText).toHaveBeenCalledWith("Overview\n\nA short report.");
});

test("retrying a markdown assistant response resends the previous user message", async () => {
  const fetchMock = persistedMarkdownChatFetch();
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Retry response" }));

  expect(await screen.findByText("Retried")).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads/t1/messages:stream",
    expect.objectContaining({
      body: JSON.stringify({ content: "Make a short report" }),
      method: "POST",
    }),
  );
});

test("user message actions copy and retry the selected prompt", async () => {
  const writeText = vi.fn();
  const fetchMock = persistedMarkdownChatFetch();
  vi.stubGlobal("navigator", { clipboard: { writeText } });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Copy message" }));
  fireEvent.click(await screen.findByRole("button", { name: "Retry message" }));

  expect(writeText).toHaveBeenCalledWith("Make a short report");
  expect(await screen.findByText("Retried")).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads/t1/messages:stream",
    expect.objectContaining({
      body: JSON.stringify({ content: "Make a short report" }),
      method: "POST",
    }),
  );
});

test("renders raw file-like assistant output inline", async () => {
  const content = "<!doctype html>\n<html><body><h1>Report</h1></body></html>";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(
    await screen.findByText(
      (_, element) =>
        element !== null &&
        element.classList.contains("spark-markdown") &&
        element.textContent?.includes("<!doctype html>") === true,
    ),
  ).toBeInTheDocument();
  expect(screen.getByText(/<html><body><h1>Report/)).toBeInTheDocument();
  expect(screen.queryByText("HTML response")).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Report" })).not.toBeInTheDocument();
});

test("renders raw generated data output inline", async () => {
  const content = "name,score\nAda,42\nGrace,37";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(await screen.findByText(/Ada,42/)).toBeInTheDocument();
  expect(screen.getByText(/Grace,37/)).toBeInTheDocument();
  expect(screen.queryByText("CSV response")).not.toBeInTheDocument();
});

test("shows fenced file-like assistant output as a dedicated download response", async () => {
  const content = "```html\n<!doctype html>\n<html><body><h1>Report</h1></body></html>\n```";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(await screen.findByText("HTML response")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Download HTML response" })).toBeInTheDocument();
  expect(screen.queryByText(/doctype html/i)).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Report" })).not.toBeInTheDocument();
});

test("does not render partial streamed HTML artifact content inline", async () => {
  let streamController: ReadableStreamDefaultController<Uint8Array> | null = null;
  const encoder = new TextEncoder();
  const content = "```html\n<!doctype html>\n<html><body><h1>Report</h1></body></html>\n```";
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController = controller;
      controller.enqueue(
        encoder.encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
      controller.enqueue(encoder.encode('event: assistant_delta\ndata: {"content":"```html\\n<!doctype html>\\n<html>"}\n\n'));
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  await sendMessageInExistingChat();

  expect(await screen.findByText("HTML response")).toBeInTheDocument();
  expect(screen.getByText("Receiving file...")).toBeInTheDocument();
  expect(screen.queryByText(/doctype html/i)).not.toBeInTheDocument();

  streamController?.enqueue(
    encoder.encode(
      `event: assistant_message\ndata: ${JSON.stringify({
        id: "m2",
        threadId: "t1",
        role: "assistant",
        content,
        createdAt: "2026-05-30T00:00:01Z",
      })}\n\n`,
    ),
  );
  streamController?.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
  streamController?.close();

  expect(await screen.findByRole("button", { name: "Download HTML response" })).toBeInTheDocument();
  expect(screen.queryByText("Receiving file...")).not.toBeInTheDocument();
  expect(screen.queryByText(/doctype html/i)).not.toBeInTheDocument();
});

test("shows a fenced HTML artifact as a download while keeping the surrounding prose", async () => {
  const content = "Here is the HTML:\n\n```html\n<!doctype html>\n<html><body><h1>Report</h1></body></html>\n```";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(await screen.findByText("HTML response")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Download HTML response" })).toBeInTheDocument();
  expect(screen.getByText(/Here is the HTML/i)).toBeInTheDocument();
  expect(screen.queryByText(/doctype html/i)).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Report" })).not.toBeInTheDocument();
});

test("keeps prose before and after a fenced artifact around the download card", async () => {
  const content = "Intro line.\n\n```html\n<!doctype html>\n<html><body><h1>Report</h1></body></html>\n```\n\nClosing remark.";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(await screen.findByText("HTML response")).toBeInTheDocument();
  expect(screen.getByText(/Intro line\./)).toBeInTheDocument();
  expect(screen.getByText(/Closing remark\./)).toBeInTheDocument();
  expect(screen.queryByText(/doctype html/i)).not.toBeInTheDocument();
});

test("renders inline triple backticks without treating them as a download fence", async () => {
  const content = "Keep this inline: ```html\n<strong>not an artifact</strong>\n```";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(await screen.findByText(/Keep this inline/)).toBeInTheDocument();
  expect(screen.queryByText("HTML response")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Download HTML response" })).not.toBeInTheDocument();
});

test("downloads fenced generated data without markdown fences", async () => {
  const objectURL = "blob:spark-response";
  let downloadedBlob: Blob | undefined;
  const createObjectURL = vi.fn((blob: Blob) => {
    downloadedBlob = blob;
    return objectURL;
  });
  const revokeObjectURL = vi.fn();
  vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL });
  const content = "```csv\nname,score\nAda,42\nGrace,37\n```";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();
  fireEvent.click(await screen.findByRole("button", { name: "Download CSV response" }));

  expect(createObjectURL).toHaveBeenCalledTimes(1);
  expect(revokeObjectURL).toHaveBeenCalledWith(objectURL);
  const blob = downloadedBlob;
  expect(blob).toBeInstanceOf(Blob);
  if (blob === undefined) throw new Error("expected download blob");
  await expect(blob.text()).resolves.toBe("name,score\nAda,42\nGrace,37");
});

test("renders invalid streaming data URLs inline until they can be decoded", async () => {
  const content = "data:text/html;base64,%%%";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(await screen.findByText(/data:text\/html;base64/)).toBeInTheDocument();
  expect(screen.queryByText("HTML response")).not.toBeInTheDocument();
});

test("shows generated office artifacts as a dedicated download response", async () => {
  const content =
    "data:application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;base64,UEsDBAo=";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(await screen.findByText("XLSX response")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Download XLSX response" })).toBeInTheDocument();
  expect(screen.queryByText(/openxmlformats/)).not.toBeInTheDocument();
});

test("ignores stream events after switching threads", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
    },
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") {
        return Response.json([
          {
            id: "t1",
            title: "First chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
          {
            id: "t2",
            title: "Second chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
        ]);
      }
      if (url === "/api/threads/t1") {
        return Response.json({
          thread: {
            id: "t1",
            title: "First chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
          messages: [],
        });
      }
      if (url === "/api/threads/t2") {
        return Response.json({
          thread: {
            id: "t2",
            title: "Second chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
          messages: [],
        });
      }
      if (url === "/api/threads/t1/messages:stream" && init?.method === "POST") {
        return new Response(stream, { status: 200 });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "First chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));
  fireEvent.click(await screen.findByRole("button", { name: "Second chat" }));
  const encoder = new TextEncoder();
  streamController.current?.enqueue(
    encoder.encode(
      'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Wrong thread answer","createdAt":"2026-05-30T00:00:01Z"}\n\n',
    ),
  );
  streamController.current?.close();

  expect(await screen.findByRole("heading", { name: "Second chat" })).toBeInTheDocument();
  expect(screen.queryByText("Wrong thread answer")).not.toBeInTheDocument();
});

test("surfaces the server error and keeps the draft when sending fails", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(
        encoder.encode(
          'event: tool_call\ndata: {"id":"call_1","name":"search__web","arguments":"{}"}\n\n',
        ),
      );
      controller.enqueue(encoder.encode('event: error\ndata: {"error":"llm is not configured"}\n\n'));
      controller.close();
    },
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") {
        return Response.json([
          {
            id: "t1",
            title: "Existing chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
        ]);
      }
      if (url === "/api/threads/t1") {
        return Response.json({
          thread: {
            id: "t1",
            title: "Existing chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
          messages: [],
        });
      }
      if (url === "/api/threads/t1/messages:stream" && init?.method === "POST") {
        return new Response(stream, { status: 200 });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("llm is not configured")).toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toHaveValue("Hi");
  expect(screen.queryByText("search__web")).not.toBeInTheDocument();
  expect(screen.queryByText("Running")).not.toBeInTheDocument();
});

test("keeps completed tool activity visible with the assistant answer", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(
        encoder.encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Search for updates","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
      controller.enqueue(
        encoder.encode(
          'event: tool_call\ndata: {"id":"call_1","name":"search__web","arguments":"{}"}\n\n',
        ),
      );
      controller.enqueue(
        encoder.encode(
          'event: tool_result\ndata: {"id":"call_1","name":"search__web","content":"result"}\n\n',
        ),
      );
      controller.enqueue(
        encoder.encode(
          'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"I found the update.","createdAt":"2026-05-30T00:00:01Z"}\n\n',
        ),
      );
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") {
        return Response.json([
          {
            id: "t1",
            title: "Existing chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
        ]);
      }
      if (url === "/api/threads/t1") {
        return Response.json({
          thread: {
            id: "t1",
            title: "Existing chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
          messages: [],
        });
      }
      if (url === "/api/threads/t1/messages:stream" && init?.method === "POST") {
        return new Response(stream, { status: 200 });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), {
    target: { value: "Search for updates" },
  });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("I found the update.")).toBeInTheDocument();
  expect(screen.getByText("search__web")).toBeInTheDocument();
  expect(screen.getByText("Done")).toBeInTheDocument();
});

function basicSignedInFetch(user: { role?: "admin" | "user" } = {}) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url === "/api/me") {
      return Response.json({
        id: "u1",
        username: "jan",
        role: user.role ?? "user",
        displayName: "Jan",
      });
    }
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([]);
    throw new Error(`unexpected fetch ${url}`);
  });
}

function threadFixture() {
  return {
    id: "t1",
    title: "Existing chat",
    starred: false,
    createdAt: "2026-05-30T00:00:00Z",
    updatedAt: "2026-05-30T00:00:00Z",
  };
}

function greetingPattern(name: string) {
  return new RegExp(`^(Morning|Afternoon|Evening), ${name}$`);
}
