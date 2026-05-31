import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, test, vi } from "vitest";
import App from "./App";

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
  expect(screen.getByText("Jan")).toBeInTheDocument();
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

test("does not double-submit new chat creation", async () => {
  let resolveCreateThread: (response: Response) => void = () => undefined;
  const createThreadResponse = new Promise<Response>((resolve) => {
    resolveCreateThread = resolve;
  });
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json([]);
    if (url === "/api/threads" && init?.method === "POST") return createThreadResponse;
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
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  const button = await screen.findByRole("button", { name: /new chat/i });
  fireEvent.click(button);
  fireEvent.click(button);
  resolveCreateThread(
    new Response(
      JSON.stringify({
        id: "t1",
        title: "New chat",
        starred: false,
        createdAt: "2026-05-30T00:00:00Z",
        updatedAt: "2026-05-30T00:00:00Z",
      }),
      { status: 201 },
    ),
  );

  expect(await screen.findByRole("heading", { name: "New chat" })).toBeInTheDocument();
  expect(
    fetchMock.mock.calls.filter(([url, init]) => String(url) === "/api/threads" && init?.method === "POST"),
  ).toHaveLength(1);
});

test("creates a new chat from the sidebar", async () => {
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
    if (url === "/api/threads/t1") {
      return Response.json({
        thread: {
          id: "t1",
          title: "New chat",
          starred: false,
          createdAt: "2026-05-30T00:00:00Z",
          updatedAt: "2026-05-30T00:00:00Z",
        },
        messages: [
          {
            id: "m1",
            threadId: "t1",
            role: "assistant",
            content: "Loaded from server",
            createdAt: "2026-05-30T00:00:01Z",
          },
        ],
      });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: /new chat/i }));

  expect(await screen.findByText("Loaded from server")).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads",
    expect.objectContaining({ method: "POST" }),
  );
  expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1");
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

  expect(await screen.findByRole("heading", { name: "New chat" })).toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toBeInTheDocument();
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

test("shows an error when message streaming fails", async () => {
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
        return Response.json({ error: "llm unavailable" }, { status: 503 });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("Message failed to send.")).toBeInTheDocument();
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
