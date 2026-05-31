import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
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
  fireEvent.change(screen.getByPlaceholderText(/project name/i), { target: { value: "School" } });
  fireEvent.click(screen.getByRole("button", { name: "Create" }));

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
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
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

test("creates the sidebar chat only after the first response title event", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(
        encoder.encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"It is hot","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
      controller.enqueue(
        encoder.encode(
          'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Drink water.","createdAt":"2026-05-30T00:00:01Z"}\n\n',
        ),
      );
      controller.enqueue(
        encoder.encode(
          'event: thread\ndata: {"id":"t1","title":"Weather comfort","starred":false,"createdAt":"2026-05-30T00:00:00Z","updatedAt":"2026-05-30T00:00:02Z","lastMessageAt":"2026-05-30T00:00:01Z"}\n\n',
        ),
      );
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
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

  expect(await screen.findByText("Drink water.")).toBeInTheDocument();
  expect(window.location.pathname).toBe("/chat/t1");
  expect(screen.queryByRole("button", { name: "New chat" })).not.toBeInTheDocument();
  expect(await screen.findByRole("button", { name: "Weather comfort" })).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads",
    expect.objectContaining({ method: "POST" }),
  );
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads/t1/messages:stream",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ content: "It is hot" }),
    }),
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

const assistantMessageEvent =
  'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Hello","createdAt":"2026-05-30T00:00:01Z"}\n\n';

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
