/// <reference types="node" />
import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { afterEach, beforeEach, test, vi } from "vitest";
import App from "./App";
import { GeneratedArtifactCard } from "./ChatShell";
import { ICONS } from "./chat/Icon";

beforeEach(() => {
  window.history.replaceState({}, "", "/");
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: memoryStorage(),
  });
});

let restoreURLObjectMethods: (() => void) | null = null;

afterEach(() => {
  restoreURLObjectMethods?.();
  restoreURLObjectMethods = null;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function stubURLObjectMethods(createObjectURL: (blob: Blob | MediaSource) => string, revokeObjectURL: (url: string) => void) {
  const originalCreateObjectURL = URL.createObjectURL;
  const originalRevokeObjectURL = URL.revokeObjectURL;
  Object.defineProperty(URL, "createObjectURL", { configurable: true, value: createObjectURL });
  Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: revokeObjectURL });
  restoreURLObjectMethods = () => {
    Object.defineProperty(URL, "createObjectURL", { configurable: true, value: originalCreateObjectURL });
    Object.defineProperty(URL, "revokeObjectURL", { configurable: true, value: originalRevokeObjectURL });
  };
}

function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear() {
      values.clear();
    },
    getItem(key: string) {
      return values.get(key) ?? null;
    },
    key(index: number) {
      return Array.from(values.keys())[index] ?? null;
    },
    removeItem(key: string) {
      values.delete(key);
    },
    setItem(key: string, value: string) {
      values.set(key, value);
    },
  };
}

test("renders signed-out screen when /api/me returns 401", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("", { status: 401 })));

  render(<App />);

  expect(await screen.findByRole("link", { name: /sign in/i })).toHaveAttribute(
    "href",
    "/api/auth/login",
  );
  expect(screen.getByAltText("Slopr")).toBeInTheDocument();
});

test("renders authenticated shell for signed-in users", async () => {
  vi.stubGlobal("fetch", basicSignedInFetch());

  render(<App />);

  expect(await screen.findByRole("button", { name: /new chat/i })).toBeInTheDocument();
  expect(await screen.findByText(greetingPattern("Jan"))).toBeInTheDocument();
  expect(screen.getByText("Jan")).toBeInTheDocument();
  expect(screen.getByText("User")).toBeInTheDocument();
  expect(screen.queryByText("user")).not.toBeInTheDocument();
  expect(window.location.pathname).toBe("/new");
});

test("opens the artifact library from the sidebar", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") {
        return Response.json({ id: "u1", username: "jan", role: "user", displayName: "Jan" });
      }
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
      if (url === "/api/mcp/status") return Response.json({ active: 0, configured: 0 });
      if (url === "/api/artifacts?type=all&sort=modified&order=desc&limit=50") {
        return Response.json({
          items: [
            {
              id: "art_1",
              threadId: "t1",
              displayFilename: "robot.png",
              mimeType: "image/png",
              sizeBytes: 1024,
              modifiedAt: "2026-06-10T12:00:00Z",
              downloadUrl: "/api/artifacts/art_1/download",
            },
          ],
          nextCursor: null,
        });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  fireEvent.click(await screen.findByRole("button", { name: "Artifacts" }));

  expect(await screen.findByRole("heading", { name: "Artifacts" })).toBeInTheDocument();
  expect(await screen.findByText("robot.png")).toBeInTheDocument();
  expect(window.location.pathname).toBe("/artifacts");
});

test("places library before projects in the primary sidebar navigation", async () => {
  vi.stubGlobal("fetch", basicSignedInFetch());

  render(<App />);

  const artifactsButton = await screen.findByRole("button", { name: "Artifacts" });
  const projectsItem = screen.getByRole("button", { name: "Projects" });

  expect(projectsItem).not.toBeNull();
  expect(artifactsButton.compareDocumentPosition(projectsItem as Element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

test("places project rows below starred chats with matching chat row sizing", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([projectFixture()]);
      if (url === "/api/threads?limit=30") {
        return Response.json({ items: [{ ...threadFixture(), starred: true }], nextCursor: null });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  const projectRow = await screen.findByRole("button", { name: "Research" });
  const starredHeading = screen.getByText("Starred");
  const chatRow = screen.getAllByRole("button", { name: "Existing chat" })[0];

  expect(starredHeading.compareDocumentPosition(projectRow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  const projectRowSurface = projectRow.parentElement as HTMLElement;
  expect(projectRowSurface).toHaveClass("h-7");
  expect(projectRow).not.toHaveClass("text-xs");
  expect(chatRow).toHaveClass("h-7");
});

test("inactive sidebar project actions stay hidden until hover or keyboard focus", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([projectFixture()]);
      if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  const actionButton = await screen.findByRole("button", { name: "Open project actions" });

  expect(actionButton).toHaveClass("invisible");
  expect(actionButton).toHaveClass("group-hover:visible");
  expect(actionButton).toHaveClass("group-focus-within:visible");
  expect(actionButton).toHaveClass("[@media(hover:none)]:visible");
});

test("greets signed-in users with up late after 22:00", async () => {
  const realDate = Date;
  type DateArgs = [] | [string | number | Date] | [number, number, number?, number?, number?, number?, number?];
  class LateDate extends realDate {
    constructor(...args: DateArgs) {
      if (args.length === 0) {
        super(2026, 5, 8, 22, 0, 0);
        return;
      }
      if (args.length === 1) {
        super(args[0]);
        return;
      }
      super(...args);
    }

    static now() {
      return new realDate(2026, 5, 8, 22, 0, 0).getTime();
    }
  }
  vi.stubGlobal("Date", LateDate);
  vi.stubGlobal("fetch", basicSignedInFetch());

  render(<App />);

  expect(await screen.findByText("Up late, Jan?")).toBeInTheDocument();
});

test("uses Loom as the HTML title", () => {
  for (const path of ["../index.html", "../../backend/web/dist/index.html"]) {
    expect(readFileSync(new URL(path, import.meta.url), "utf8")).toContain("<title>Loom</title>");
  }
});

test("uses Loom as the web app manifest title", () => {
  const manifest = JSON.parse(readFileSync("public/site.webmanifest", "utf8")) as {
    name?: string;
    short_name?: string;
  };

  expect(manifest.name).toBe("Loom");
  expect(manifest.short_name).toBe("Loom");
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
        return Response.json({ items: [
          {
            ...threadFixture(),
            title: "Albert Einstein The legendary physicist who revolutionized modern physics",
          },
        ], nextCursor: null });
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

test("shows MCP status on the new chat screen", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") {
        return Response.json({ id: "u1", username: "jan", role: "user", displayName: "Jan" });
      }
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
      if (url === "/api/mcp/status") {
        return Response.json({
          active: 3,
          configured: 4,
          servers: [
            { name: "fetch", active: true },
            { name: "obscura", active: false },
            { name: "tavily", active: true },
            { name: "context7", active: true },
          ],
        });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  expect(await screen.findByPlaceholderText("How can I help you today?")).toBeInTheDocument();
  const header = screen.getByRole("banner", { name: "Chat header" });
  const indicator = await within(header).findByTitle("3 of 4 MCP servers active. Failed: obscura");
  expect(indicator).toHaveTextContent("3");
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
      if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
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
        return Response.json({ items: [
          {
            id: "t1",
            title: "Algebra",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
        ], nextCursor: null });
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
      if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  expect(await screen.findByText("Chat data failed to load.")).toBeInTheDocument();
});

test("does not expose project creation from the sidebar", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects" && init?.method === undefined) return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);

  await screen.findByRole("button", { name: "New chat" });
  expect(screen.getByRole("button", { name: "Memories" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Memory" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /new project/i })).not.toBeInTheDocument();
  expect(screen.queryByPlaceholderText(/project name/i)).not.toBeInTheDocument();
  expect(fetchMock).not.toHaveBeenCalledWith("/api/projects", expect.objectContaining({ method: "POST" }));
});

test("opens the projects page from the sidebar without example or share affordances", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([projectFixture()]);
      if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Projects" }));

  expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
  expect(screen.getAllByRole("button", { name: "Research" }).length).toBeGreaterThan(0);
  expect(screen.queryByText("Example project")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Share" })).not.toBeInTheDocument();
});

test("loads a project detail page and creates new chats inside the project", async () => {
  window.history.replaceState({}, "", "/projects/p1");
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(
        new TextEncoder().encode(
          'event: thread\ndata: {"id":"t-project-new","projectId":"p1","title":"Project brief","starred":false,"createdAt":"2026-05-30T00:00:00Z","updatedAt":"2026-05-30T00:00:01Z"}\n\n' +
            'event: assistant_message\ndata: {"id":"m1","threadId":"t-project-new","role":"assistant","content":"Done","createdAt":"2026-05-30T00:00:01Z"}\n\n' +
            "event: done\ndata: {}\n\n",
        ),
      );
      controller.close();
    },
  });
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([projectFixture()]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
    if (url === "/api/threads?projectId=p1&limit=1000") {
      return Response.json({ items: [{ ...threadFixture(), id: "t-project", title: "Project chat", projectId: "p1" }], nextCursor: null });
    }
    if (url === "/api/threads" && init?.method === "POST") {
      const body = JSON.parse(String(init.body)) as { title?: string };
      return Response.json({
        id: "t-project-new",
        projectId: "p1",
        title: body.title ?? "New chat",
        starred: false,
        createdAt: "2026-05-30T00:00:00Z",
        updatedAt: "2026-05-30T00:00:00Z",
      });
    }
    if (url === "/api/threads/t-project-new/messages:stream" && init?.method === "POST") {
      return new Response(stream, { status: 200 });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);

  expect(await screen.findByRole("heading", { name: "Research" })).toBeInTheDocument();
  expect(await screen.findByRole("button", { name: /Project chat/ })).toBeInTheDocument();
  expect(screen.queryByText("Files")).not.toBeInTheDocument();
  expect(window.location.pathname).toBe("/projects/p1");
  const projectComposer = screen.getByPlaceholderText("How can I help you today?");
  expect(projectComposer).toHaveFocus();
  fireEvent.change(projectComposer, {
    target: { value: "Draft a brief" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));

  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ projectId: "p1", title: "Draft a brief" }),
      }),
    ),
  );
  await waitFor(() => expect(window.location.pathname).toBe("/chat/t-project-new"));
});

test("adds a single chat to a project from the chat actions menu", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([projectFixture()]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [threadFixture()], nextCursor: null });
    if (url === "/api/threads/t1") {
      if (init?.method === "PATCH") return Response.json({ ...threadFixture(), projectId: "p1" });
      return Response.json({ thread: threadFixture(), messages: [] });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Add to project" }));
  fireEvent.click(within(await screen.findByRole("dialog", { name: "Add to project" })).getByRole("button", { name: "Research" }));

  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t1",
      expect.objectContaining({
        method: "PATCH",
        body: JSON.stringify({ projectId: "p1" }),
      }),
    ),
  );
  expect(screen.queryByRole("dialog", { name: "Add to project" })).not.toBeInTheDocument();
});

test("moves selected chats to a project from the chats page", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([projectFixture()]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
    if (url === "/api/threads?limit=50") {
      return Response.json({ items: [
        { ...threadFixture(), id: "t1", title: "Loose chat one" },
        { ...threadFixture(), id: "t2", title: "Loose chat two" },
      ], nextCursor: null });
    }
    if (url === "/api/threads/ids") return Response.json(["t1", "t2"]);
    if (url === "/api/threads/t1" && init?.method === "PATCH") {
      return Response.json({ ...threadFixture(), id: "t1", title: "Loose chat one", projectId: "p1" });
    }
    if (url === "/api/threads/t2" && init?.method === "PATCH") {
      return Response.json({ ...threadFixture(), id: "t2", title: "Loose chat two", projectId: "p1" });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Chats" }));
  await screen.findByText("Loose chat one");
  fireEvent.click(screen.getByRole("button", { name: "Select chats" }));
  fireEvent.click(screen.getByRole("button", { name: "Select all" }));
  await screen.findByText("2 selected");
  fireEvent.click(screen.getByRole("button", { name: "Move to project" }));
  fireEvent.click(within(await screen.findByRole("dialog", { name: "Move to project" })).getByRole("button", { name: "Research" }));

  await waitFor(() => {
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t1",
      expect.objectContaining({ method: "PATCH", body: JSON.stringify({ projectId: "p1" }) }),
    );
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t2",
      expect.objectContaining({ method: "PATCH", body: JSON.stringify({ projectId: "p1" }) }),
    );
  });
  expect(screen.queryByRole("dialog", { name: "Move to project" })).not.toBeInTheDocument();
});

test("renders the new-chat plus icon without a new-project sidebar control", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);

  const newChatButton = await screen.findByRole("button", { name: "New chat" });

  // New chat: a thin SVG plus inside a circle (no literal "+").
  expect(newChatButton.querySelector("svg")).toBeInTheDocument();
  expect(newChatButton.querySelector("svg")).toHaveClass("h-[13px]", "w-[13px]");
  expect(newChatButton).not.toHaveTextContent("+");
  expect(screen.queryByRole("button", { name: "New project" })).not.toBeInTheDocument();
});

test("new chat navigation does not create a thread or sidebar entry", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [{ ...threadFixture(), id: "existing", title: "Existing chat" }], nextCursor: null });
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
    if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
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
  expect(
    within(screen.getByText("Recents").closest("section")!).queryByRole("button", { name: "New chat" }),
  ).not.toBeInTheDocument();
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

test("sends a deferred new-chat image with the first prompt and shows the prompt as the initial chat title", async () => {
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(
        encoder.encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"What is this image?","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
      controller.enqueue(
        encoder.encode('event: assistant_delta\ndata: {"content":"It is a small PNG."}\n\n'),
      );
      controller.enqueue(
        encoder.encode(
          'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"It is a small PNG.","createdAt":"2026-05-30T00:00:01Z"}\n\n',
        ),
      );
      controller.close();
    },
  });
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
    if (url === "/api/threads" && init?.method === "POST") {
      const body = JSON.parse(String(init.body)) as { title?: string };
      return Response.json(
        {
          id: "t1",
          title: body.title ?? "New chat",
          starred: false,
          createdAt: "2026-05-30T00:00:00Z",
          updatedAt: "2026-05-30T00:00:00Z",
        },
        { status: 201 },
      );
    }
    if (url === "/api/artifacts/images/upload" && init?.method === "POST") {
      return Response.json({
        id: "art_image",
        threadId: "t1",
        displayFilename: "tiny.png",
        mimeType: "image/png",
        sizeBytes: 68,
        createdAt: "2026-05-30T00:00:00Z",
        downloadUrl: "/api/artifacts/art_image/download",
      });
    }
    if (url === "/api/threads/t1/messages:stream" && init?.method === "POST") return new Response(stream, { status: 200 });
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  stubURLObjectMethods(() => "blob:tiny", () => undefined);

  render(<App />);
  const textbox = await screen.findByPlaceholderText("How can I help you today?");
  const composer = textbox.closest("form");
  const fileInput = composer?.querySelector('input[type="file"]');
  if (fileInput === null || fileInput === undefined) throw new Error("file input missing");
  fireEvent.change(fileInput, {
    target: {
      files: [new File(["png"], "tiny.png", { type: "image/png" })],
    },
  });
  fireEvent.change(textbox, { target: { value: "What is this image?" } });
  fireEvent.click(screen.getByRole("button", { name: /send message/i }));

  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads",
    expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ title: "What is this image?" }),
    }),
  );
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t1/messages:stream",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ content: "What is this image?", imageAttachmentIds: ["art_image"] }),
      }),
    ),
  );
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/artifacts/images/upload",
    expect.objectContaining({ method: "POST" }),
  );
  expect(document.querySelector('img[src="blob:tiny"]')).toBeInTheDocument();
  expect(screen.queryByText("tiny.png")).not.toBeInTheDocument();
  expect(screen.queryByText("Files must be 25 MB or smaller.")).not.toBeInTheDocument();
  expect(screen.queryByText("Attached tiny.png.")).not.toBeInTheDocument();
  expect(await screen.findByRole("button", { name: "What is this image?" })).toBeInTheDocument();
});

test("retries a failed deferred new-chat image upload before streaming", async () => {
  let createThreadCalls = 0;
  let imageUploadCalls = 0;
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(
        encoder.encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t2","role":"user","content":"What is this image?","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
      controller.close();
    },
  });
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
    if (url === "/api/threads" && init?.method === "POST") {
      createThreadCalls += 1;
      return Response.json(
        {
          id: `t${createThreadCalls}`,
          title: "What is this image?",
          starred: false,
          createdAt: "2026-05-30T00:00:00Z",
          updatedAt: "2026-05-30T00:00:00Z",
        },
        { status: 201 },
      );
    }
    if (url === "/api/artifacts/images/upload" && init?.method === "POST") {
      imageUploadCalls += 1;
      if (imageUploadCalls === 1) return new Response("boom", { status: 500 });
      return Response.json({
        id: "art_image_retry",
        threadId: "t2",
        displayFilename: "tiny.png",
        mimeType: "image/png",
        sizeBytes: 68,
        createdAt: "2026-05-30T00:00:00Z",
        downloadUrl: "/api/artifacts/art_image_retry/download",
      });
    }
    if (url === "/api/threads/t2/messages:stream" && init?.method === "POST") {
      return new Response(stream, { status: 200 });
    }
    if (url.endsWith("/messages:stream") && init?.method === "POST") {
      throw new Error(`unexpected stream ${url}`);
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  stubURLObjectMethods(() => "blob:tiny", () => undefined);

  render(<App />);
  const textbox = await screen.findByPlaceholderText("How can I help you today?");
  const composer = textbox.closest("form");
  const fileInput = composer?.querySelector('input[type="file"]');
  if (fileInput === null || fileInput === undefined) throw new Error("file input missing");
  fireEvent.change(fileInput, {
    target: {
      files: [new File(["png"], "tiny.png", { type: "image/png" })],
    },
  });
  fireEvent.change(textbox, { target: { value: "What is this image?" } });
  fireEvent.click(screen.getByRole("button", { name: /send message/i }));

  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/artifacts/images/upload",
      expect.objectContaining({ method: "POST" }),
    ),
  );
  await waitFor(() => expect(screen.getByText("failed to upload image")).toBeInTheDocument());
  expect(
    fetchMock.mock.calls.some(([url, init]) => String(url) === "/api/threads/t1/messages:stream" && init?.method === "POST"),
  ).toBe(false);
  expect(screen.queryByText("Files must be 25 MB or smaller.")).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: /send message/i }));

  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t2/messages:stream",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ content: "What is this image?", imageAttachmentIds: ["art_image_retry"] }),
      }),
    ),
  );
  expect(imageUploadCalls).toBe(2);
  expect(window.location.pathname).toBe("/chat/t2");
});

test("clears a stale upload size send error when a new valid file is attached on the start screen", async () => {
  let imageUploadCalls = 0;
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
    if (url === "/api/threads" && init?.method === "POST") {
      return Response.json(
        {
          id: `t${imageUploadCalls + 1}`,
          title: "What is this image?",
          starred: false,
          createdAt: "2026-05-30T00:00:00Z",
          updatedAt: "2026-05-30T00:00:00Z",
        },
        { status: 201 },
      );
    }
    if (url === "/api/artifacts/images/upload" && init?.method === "POST") {
      imageUploadCalls += 1;
      if (imageUploadCalls === 1) return new Response("too large", { status: 413 });
      return Response.json({
        id: "art_valid",
        threadId: "t2",
        displayFilename: "valid.png",
        mimeType: "image/png",
        sizeBytes: 1_400_000,
        createdAt: "2026-05-30T00:00:00Z",
        downloadUrl: "/api/artifacts/art_valid/download",
      });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  stubURLObjectMethods(() => "blob:preview", () => undefined);

  render(<App />);
  const textbox = await screen.findByPlaceholderText("How can I help you today?");
  const composer = textbox.closest("form");
  const fileInput = composer?.querySelector('input[type="file"]');
  if (fileInput === null || fileInput === undefined) throw new Error("file input missing");

  fireEvent.change(fileInput, {
    target: {
      files: [new File(["png"], "first.png", { type: "image/png" })],
    },
  });
  fireEvent.change(textbox, { target: { value: "What is this image?" } });
  fireEvent.click(screen.getByRole("button", { name: /send message/i }));

  expect(await screen.findByText("Files must be 25 MB or smaller.")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Remove first.png" }));
  fireEvent.change(fileInput, {
    target: {
      files: [new File(["x".repeat(1_400_000)], "valid.png", { type: "image/png" })],
    },
  });

  expect(screen.queryByText("Files must be 25 MB or smaller.")).not.toBeInTheDocument();
  expect(screen.getByText("valid.png")).toBeInTheDocument();
});

test("active sidebar chat shows actions menu with locked entries", async () => {
  vi.stubGlobal(
    "fetch",
    chatThreadFetch(null, [{ id: "m1", role: "assistant", content: "Earlier answer" }]),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));

  expect(await screen.findByRole("menu", { name: "Chat actions" })).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: /^Star$/ })).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: "Rename" })).toBeInTheDocument();
  expect(screen.getByRole("menuitem", { name: "Add to project" })).toBeDisabled();
  expect(screen.getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
});

test("active chat header chevron opens the shared chat actions menu", async () => {
  vi.stubGlobal(
    "fetch",
    chatThreadFetch(null, [{ id: "m1", role: "assistant", content: "Earlier answer" }]),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const header = await screen.findByRole("banner", { name: "Chat header" });
  fireEvent.click(within(header).getByRole("button", { name: "Open chat actions" }));

  const menu = await screen.findByRole("menu", { name: "Chat actions" });
  expect(within(menu).getByRole("menuitem", { name: /^Star$/ })).toBeInTheDocument();
  expect(within(menu).getByRole("menuitem", { name: "Rename" })).toBeInTheDocument();
  expect(within(menu).getByRole("menuitem", { name: "Add to project" })).toBeDisabled();
  expect(within(menu).getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();

  fireEvent.click(within(menu).getByRole("menuitem", { name: "Rename" }));

  expect(await screen.findByRole("dialog", { name: "Rename chat" })).toBeInTheDocument();
});

test("project chat header prefixes the title with a clickable project name", async () => {
  const project = { ...projectFixture(), name: "AI Gateway Comparison" };
  const thread = { ...threadFixture(), title: "Inference Spend Statistics Access", projectId: project.id };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([project]);
      if (url === "/api/threads?limit=30") return Response.json({ items: [thread], nextCursor: null });
      if (url === "/api/threads/t1") return Response.json({ thread, messages: [] });
      if (url === "/api/threads?projectId=p1&limit=1000") {
        return Response.json({ items: [thread], nextCursor: null });
      }
      if (url === "/api/projects/p1/memory") {
        return Response.json({ projectId: "p1", content: "", updatedAt: null });
      }
      if (url === "/api/mcp/status") return Response.json({ active: 0, configured: 0 });
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Inference Spend Statistics Access" }));

  const header = await screen.findByRole("banner", { name: "Chat header" });
  const headerTitle = within(header).getByRole("heading", { name: /AI Gateway Comparison.*Inference Spend Statistics Access/ });
  expect(headerTitle).toBeInTheDocument();
  expect(headerTitle).not.toHaveTextContent(">");
  expect(headerTitle).toHaveTextContent(ICONS.chevronRight);

  fireEvent.click(within(header).getByRole("button", { name: "AI Gateway Comparison" }));

  expect(await screen.findByRole("heading", { name: "AI Gateway Comparison" })).toBeInTheDocument();
  expect(window.location.pathname).toBe("/projects/p1");
});

test("closes the active sidebar chat menu when clicking outside it", async () => {
  vi.stubGlobal(
    "fetch",
    chatThreadFetch(null, [{ id: "m1", role: "assistant", content: "Earlier answer" }]),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  expect(await screen.findByRole("menu", { name: "Chat actions" })).toBeInTheDocument();

  fireEvent.pointerDown(screen.getByRole("main"));

  expect(screen.queryByRole("menu", { name: "Chat actions" })).not.toBeInTheDocument();
});

test("add to project stays disabled until projects exist", async () => {
  const fetchMock = chatThreadFetch(null);
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));

  expect(await screen.findByRole("menuitem", { name: "Add to project" })).toBeDisabled();
  expect(fetchMock.mock.calls.filter(([url]) => String(url).includes("project"))).toHaveLength(1);
});

test("stars and unstars a chat from the sidebar action menu and closes the menu", async () => {
  let starred = false;
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [{ ...threadFixture(), starred }], nextCursor: null });
    if (url === "/api/threads/t1") {
      return Response.json({ thread: { ...threadFixture(), starred }, messages: [] });
    }
    if (url === "/api/threads/t1/star" && init?.method === "POST") {
      starred = true;
      return Response.json({ ...threadFixture(), starred: true });
    }
    if (url === "/api/threads/t1/unstar" && init?.method === "POST") {
      starred = false;
      return Response.json({ ...threadFixture(), starred: false });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Star" }));
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1/star", { method: "POST" }),
  );
  expect(screen.queryByRole("menu", { name: "Chat actions" })).not.toBeInTheDocument();

  fireEvent.click(screen.getAllByRole("button", { name: "Open chat actions" })[0]);
  fireEvent.click(await screen.findByRole("menuitem", { name: "Unstar" }));
  await waitFor(() =>
    expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1/unstar", { method: "POST" }),
  );
  expect(screen.queryByRole("menu", { name: "Chat actions" })).not.toBeInTheDocument();
});

test("renames a chat from the sidebar menu", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") {
      return Response.json({ items: [{ ...threadFixture(), title: "Existing chat" }], nextCursor: null });
    }
    if (url === "/api/threads/t1" && init?.method === "PATCH") {
      return Response.json({ ...threadFixture(), title: "Renamed chat" });
    }
    if (url === "/api/threads/t1") {
      return Response.json({
        thread: { ...threadFixture(), title: "Existing chat" },
        messages: [],
      });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Rename" }));

  expect(await screen.findByRole("dialog", { name: "Rename chat" })).toBeInTheDocument();
  const input = await screen.findByRole("textbox", { name: "Chat title" });
  expect(input).toHaveValue("Existing chat");
  fireEvent.change(input, { target: { value: "Renamed chat" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));

  expect(await screen.findByRole("button", { name: "Renamed chat" })).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith(
    "/api/threads/t1",
    expect.objectContaining({
      method: "PATCH",
      body: JSON.stringify({ title: "Renamed chat" }),
    }),
  );
});

test("deletes the active chat from the sidebar menu after confirmation", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") {
      return Response.json({ items: [{ ...threadFixture(), title: "Existing chat" }], nextCursor: null });
    }
    if (url === "/api/threads/t1" && init?.method === "DELETE") {
      return new Response(null, { status: 204 });
    }
    if (url === "/api/threads/t1") {
      return Response.json({
        thread: { ...threadFixture(), title: "Existing chat" },
        messages: [],
      });
    }
    throw new Error(`unexpected fetch ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Delete" }));

  expect(await screen.findByRole("dialog", { name: "Delete chat" })).toBeInTheDocument();
  expect(screen.getByText("Are you sure you want to delete this chat?")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));

  await waitFor(() => expect(window.location.pathname).toBe("/new"));
  expect(screen.queryByRole("button", { name: "Existing chat" })).not.toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledWith("/api/threads/t1", { method: "DELETE" });
});

test("closes a chat modal when clicking the backdrop", async () => {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") {
      return Response.json({ items: [{ ...threadFixture(), title: "Existing chat" }], nextCursor: null });
    }
    if (url === "/api/threads/t1") {
      return Response.json({
        thread: { ...threadFixture(), title: "Existing chat" },
        messages: [],
      });
    }
    throw new Error(`unexpected fetch ${url} ${init?.method ?? "GET"}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.click(await screen.findByRole("button", { name: "Open chat actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Rename" }));

  const dialog = await screen.findByRole("dialog", { name: "Rename chat" });
  fireEvent.click(dialog.parentElement!);

  expect(screen.queryByRole("dialog", { name: "Rename chat" })).not.toBeInTheDocument();
});

test("shows MCP status in the active chat header without the header star action", async () => {
  const thread = () => ({
    id: "t1",
    title: "Existing chat",
    starred: false,
    createdAt: "2026-05-30T00:00:00Z",
    updatedAt: "2026-05-30T00:00:00Z",
  });
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [thread()], nextCursor: null });
    if (url === "/api/threads/t1") return Response.json({ thread: thread(), messages: [] });
    if (url === "/api/mcp/status") return Response.json({ active: 2, configured: 3 });
    throw new Error(`unexpected fetch ${url}`);
  }));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const header = await screen.findByRole("banner", { name: "Chat header" });
  expect(within(header).getByTitle("2 of 3 MCP servers active")).toBeInTheDocument();
  expect(within(header).queryByRole("button", { name: /star chat/i })).toBeNull();
});

test("starting chat exits the admin panel", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "admin" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
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
        return Response.json({ items: [
          {
            id: "t1",
            title: "Existing chat",
            starred: false,
            createdAt: "2026-05-30T00:00:00Z",
            updatedAt: "2026-05-30T00:00:00Z",
          },
        ], nextCursor: null });
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

test("turns the send button into a stop button while the assistant is running", async () => {
  const fetchMock = stoppingChatFetch();
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));

  const stopButton = await screen.findByRole("button", { name: "Stop response" });
  expect(stopButton).toBeEnabled();
  expect(stopButton).toHaveClass("bg-[#3a3a37]");
  expect(stopButton).not.toHaveClass("bg-accent");

  fireEvent.click(stopButton);

  await waitFor(() => {
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t1/messages:stop",
      expect.objectContaining({ method: "POST" }),
    );
  });
  await waitFor(() => {
    expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
  });
});

test("Escape stops the active assistant response", async () => {
  const fetchMock = stoppingChatFetch();
  vi.stubGlobal("fetch", fetchMock);

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));

  await screen.findByRole("button", { name: "Stop response" });
  fireEvent.keyDown(window, { key: "Escape" });

  await waitFor(() => {
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t1/messages:stop",
      expect.objectContaining({ method: "POST" }),
    );
  });
});

test("renders artifact card from streamed artifact event", async () => {
  const artifact = {
    id: "art_1",
    displayFilename: "notes.md",
    mimeType: "text/markdown; charset=utf-8",
    sizeBytes: 7,
    downloadUrl: "/api/artifacts/art_1/download",
  };
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(
        encoder.encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"make file","createdAt":"2026-06-03T00:00:00Z"}\n\n',
        ),
      );
      controller.enqueue(encoder.encode(`event: artifact\ndata: ${JSON.stringify(artifact)}\n\n`));
      controller.enqueue(
        encoder.encode(
          `event: assistant_message\ndata: ${JSON.stringify({
            id: "m2",
            threadId: "t1",
            role: "assistant",
            content: "Created notes.md.",
            artifacts: [artifact],
            createdAt: "2026-06-03T00:00:01Z",
          })}\n\n`,
        ),
      );
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "make file" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("notes.md")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Download notes.md" })).toBeInTheDocument();
});

test("renders artifact card from historical assistant message", async () => {
  vi.stubGlobal(
    "fetch",
    chatThreadFetch(null, [
      {
        id: "m1",
        role: "assistant",
        content: "Created notes.md.",
        artifacts: [
          {
            id: "art_1",
            displayFilename: "notes.md",
            mimeType: "text/markdown; charset=utf-8",
            sizeBytes: 7,
            downloadUrl: "/api/artifacts/art_1/download",
          },
        ],
      },
    ]),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  expect(await screen.findByText("notes.md")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Download notes.md" })).toBeInTheDocument();
});

test("renders image artifact preview from generated artifact card", async () => {
  const objectURL = "blob:ui-image-preview";
  const createObjectURL = vi.fn(() => objectURL);
  const revokeObjectURL = vi.fn();
  stubURLObjectMethods(createObjectURL, revokeObjectURL);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      if (String(input) === "/api/artifacts/art_1/download") {
        return {
          status: 200,
          ok: true,
          blob: async () => new Blob(["image-bytes"], { type: "image/png" }),
        } as Response;
      }
      throw new Error(`unexpected fetch ${String(input)}`);
    }),
  );

  render(
    <GeneratedArtifactCard
      artifact={{
        id: "art_1",
        displayFilename: "robot.png",
        mimeType: "image/png",
        sizeBytes: 12,
        downloadUrl: "/api/artifacts/art_1/download",
      }}
    />,
  );

  expect(await screen.findByRole("img", { name: "robot.png" }, { timeout: 3000 })).toHaveAttribute(
    "src",
    objectURL,
  );
  expect(createObjectURL).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("button", { name: "Download robot.png" })).toBeInTheDocument();
});

test("clicking an image artifact opens a lightbox preview in the browser", async () => {
  const objectURL = "blob:ui-image-preview";
  stubURLObjectMethods(vi.fn(() => objectURL), vi.fn());
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    if (String(input) === "/api/artifacts/art_1/download") {
      return {
        status: 200,
        ok: true,
        blob: async () => new Blob(["image-bytes"], { type: "image/png" }),
      } as Response;
    }
    throw new Error(`unexpected fetch ${String(input)}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  render(
    <GeneratedArtifactCard
      artifact={{
        id: "art_1",
        displayFilename: "robot.png",
        mimeType: "image/png",
        sizeBytes: 12,
        downloadUrl: "/api/artifacts/art_1/download",
      }}
    />,
  );

  fireEvent.click(await screen.findByRole("img", { name: "robot.png" }));

  // The lightbox overlay appears, showing the already-downloaded blob — no host open call.
  const dialog = await screen.findByRole("dialog", { name: "Preview robot.png" });
  expect(within(dialog).getByRole("img", { name: "robot.png" })).toHaveAttribute(
    "src",
    objectURL,
  );
  expect(fetchMock).not.toHaveBeenCalledWith(
    "/api/artifacts/art_1/open",
    expect.anything(),
  );

  // The close button dismisses the lightbox.
  fireEvent.click(within(dialog).getByRole("button", { name: "Close preview" }));
  await waitFor(() => {
    expect(screen.queryByRole("dialog", { name: "Preview robot.png" })).not.toBeInTheDocument();
  });

  // Escape also closes it.
  fireEvent.click(screen.getByRole("img", { name: "robot.png" }));
  await screen.findByRole("dialog", { name: "Preview robot.png" });
  fireEvent.keyDown(window, { key: "Escape" });
  await waitFor(() => {
    expect(screen.queryByRole("dialog", { name: "Preview robot.png" })).not.toBeInTheDocument();
  });
});

test("keeps just-completed reasoning trace collapsed until opened", async () => {
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

  expect(await screen.findByText("Answer.")).toBeInTheDocument();
  const toggle = screen.getByRole("button", { name: /show activity/i });
  expect(toggle).toBeInTheDocument();
  expect(screen.queryByText("I checked the source first.")).not.toBeInTheDocument();

  fireEvent.click(toggle);

  expect(await screen.findByText("I checked the source first.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /hide activity/i })).toBeInTheDocument();
  // Reasoning row shows the clock timeline node; the turn is capped with a Done node.
  expect(document.querySelector(".ui-activity-clock-icon")).not.toBeNull();
  expect(screen.getByText("Done")).toBeInTheDocument();
});

test("restores persisted activity trace when reopening a chat", async () => {
  vi.stubGlobal(
    "fetch",
    chatThreadFetch(null, [
      {
        id: "m1",
        role: "user",
        content: "Search Slopr",
      },
      {
        id: "m2",
        role: "assistant",
        content: "I found Slopr.",
        activityTrace: [
          {
            id: "reasoning-1",
            type: "reasoning",
            content: "I should search current sources.",
            status: "done",
          },
          {
            id: "call_1",
            type: "tool",
            name: "search__web",
            status: "done",
            rawArguments: "{\"query\":\"agentgateway kgateway\"}",
            rawOutput:
              "{\"results\":[{\"title\":\"Agentgateway\",\"url\":\"https://agentgateway.dev\",\"snippet\":\"Next generation proxy\"}]}",
          },
        ],
      },
    ]),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  expect(await screen.findByText("I found Slopr.")).toBeInTheDocument();
  const toggle = screen.getByRole("button", { name: /show activity/i });
  expect(screen.queryByText("I should search current sources.")).not.toBeInTheDocument();

  fireEvent.click(toggle);

  expect(await screen.findByText("I should search current sources.")).toBeInTheDocument();
  expect(screen.getByText("agentgateway kgateway")).toBeInTheDocument();
  expect(screen.getByText("Agentgateway")).toBeInTheDocument();
  // The reasoning row shows the clock timeline node.
  expect(document.querySelector(".ui-activity-clock-icon")).not.toBeNull();
});

test("keeps past activity traces collapsed by default, ignoring any stale stored toggle", async () => {
  // The toggle is no longer persisted; a leftover value from an older build must
  // not force past traces open.
  window.localStorage.setItem("slopr:activity-trace-expanded", "true");
  vi.stubGlobal(
    "fetch",
    chatThreadFetch(null, [
      {
        id: "m1",
        role: "user",
        content: "Search Slopr",
      },
      {
        id: "m2",
        role: "assistant",
        content: "I found Slopr.",
        activityTrace: [
          {
            id: "reasoning-1",
            type: "reasoning",
            content: "I should search current sources.",
            status: "done",
          },
        ],
      },
    ]),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  expect(await screen.findByText("I found Slopr.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /show activity/i })).toBeInTheDocument();
  expect(screen.queryByText("I should search current sources.")).not.toBeInTheDocument();
});

test("toggles a past activity trace without persisting the choice to localStorage", async () => {
  vi.stubGlobal(
    "fetch",
    chatThreadFetch(null, [
      {
        id: "m1",
        role: "user",
        content: "Search Slopr",
      },
      {
        id: "m2",
        role: "assistant",
        content: "I found Slopr.",
        activityTrace: [
          {
            id: "reasoning-1",
            type: "reasoning",
            content: "I should search current sources.",
            status: "done",
          },
        ],
      },
    ]),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const showToggle = await screen.findByRole("button", { name: /show activity/i });
  fireEvent.click(showToggle);

  expect(screen.getByText("I should search current sources.")).toBeInTheDocument();
  expect(window.localStorage.getItem("slopr:activity-trace-expanded")).toBeNull();

  fireEvent.click(screen.getByRole("button", { name: /hide activity/i }));

  await waitFor(() => {
    expect(screen.queryByText("I should search current sources.")).not.toBeInTheDocument();
  });
  expect(window.localStorage.getItem("slopr:activity-trace-expanded")).toBeNull();
});

test("keeps active activity trace while assistant output streams without explicit trace events", async () => {
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

  const trace = await screen.findByRole("status", { name: /slopr activity trace/i });
  expect(within(trace).getByText("Thinking")).toBeInTheDocument();
  // No reasoning has streamed yet: just the sweeping label, no chevron to expand.
  expect(trace.querySelector(".ui-thinking-chevron")).toBeNull();
  expect(trace.querySelector(".ui-thinking-chevron-expanded")).toBeNull();

  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Hel"}\n\n'));

  expect(await screen.findByText("Hel")).toBeInTheDocument();
  expect(screen.getByRole("status", { name: /slopr activity trace/i })).toBeInTheDocument();
  expect(screen.getByText("Thinking")).toBeInTheDocument();
});

test("shows the reasoning abstract once its background title arrives", async () => {
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

  // While reasoning streams (no answer text yet) the label shows "Thinking".
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_delta\ndata: {"content":"I should search current sources."}\n\n'),
  );
  expect(await screen.findByText("Thinking")).toBeInTheDocument();

  // The answer starts streaming, but the round has no background title yet: the
  // label must stay "Thinking" rather than flashing the raw-first-sentence
  // fallback. The assistant_message/done events are withheld so the trace stays.
  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Here is"}\n\n'));
  expect(await screen.findByText("Here is")).toBeInTheDocument();
  expect(screen.getByText("Thinking")).toBeInTheDocument();
  expect(screen.queryByText("Search current sources")).not.toBeInTheDocument();

  // The background title lands: the collapsed label flips to the abstract and the
  // live "Thinking" status is gone.
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_title\ndata: {"id":"reasoning-1","title":"Searching current sources"}\n\n'),
  );
  expect(await screen.findByText("Searching current sources")).toBeInTheDocument();
  expect(screen.queryByText("Thinking")).not.toBeInTheDocument();
  expect(screen.queryByRole("status", { name: /slopr activity trace/i })).not.toBeInTheDocument();
});

test("auto-opens the live thinking window once and keeps it open after the answer settles", async () => {
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

  // Reasoning streams: the window opens itself once, no click needed.
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_delta\ndata: {"content":"I should search current sources."}\n\n'),
  );
  expect(await screen.findByText("I should search current sources.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /hide activity/i })).toBeInTheDocument();

  // The answer settles (text + background title). The window does NOT collapse on
  // its own — it stays open through the answer for the rest of the turn; only the
  // headline label flips to the generated abstract.
  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Here is"}\n\n'));
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_title\ndata: {"id":"reasoning-1","title":"Searching current sources"}\n\n'),
  );
  expect(await screen.findByText("Searching current sources")).toBeInTheDocument();
  expect(screen.getByText("I should search current sources.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /hide activity/i })).toBeInTheDocument();
});

test("does not auto-open the live thinking window at the bare start of a turn", async () => {
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

  // The turn has begun but nothing has streamed yet: the window must stay closed —
  // no expanded chevron, no "Hide activity" affordance.
  const trace = await screen.findByRole("status", { name: /slopr activity trace/i });
  expect(within(trace).getByText("Thinking")).toBeInTheDocument();
  expect(trace.querySelector(".ui-thinking-chevron-expanded")).toBeNull();
  expect(within(trace).queryByRole("button", { name: /hide activity/i })).not.toBeInTheDocument();

  // It opens the first moment there is something to show — here, the answer text.
  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Hello"}\n\n'));
  expect(await screen.findByText("Hello")).toBeInTheDocument();
});

test("keeps the live thinking window collapsed once the user closes it mid-turn", async () => {
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

  // Reasoning streams and the window auto-opens; the user then closes it.
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_delta\ndata: {"content":"I should search current sources."}\n\n'),
  );
  fireEvent.click(await screen.findByRole("button", { name: /hide activity/i }));
  expect(await screen.findByRole("button", { name: /show activity/i })).toBeInTheDocument();

  // A later phase change (answer text + background title) must NOT re-open it: the
  // auto-open fires at most once per turn, so the manual collapse sticks.
  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Here is"}\n\n'));
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_title\ndata: {"id":"reasoning-1","title":"Searching current sources"}\n\n'),
  );
  expect(await screen.findByText("Searching current sources")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /show activity/i })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /hide activity/i })).not.toBeInTheDocument();
});

test("keeps the generated reasoning title during later active trace updates", async () => {
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

  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_delta\ndata: {"content":"I should search current sources."}\n\n'),
  );
  expect(await screen.findByText("Thinking")).toBeInTheDocument();

  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_title\ndata: {"id":"reasoning-1","title":"Searching current sources"}\n\n'),
  );
  expect(await screen.findByText("Searching current sources")).toBeInTheDocument();
  expect(screen.queryByText("Thinking")).not.toBeInTheDocument();

  streamController.current?.enqueue(
    new TextEncoder().encode('event: tool_call\ndata: {"id":"call_1","name":"fetch__fetch","arguments":"{\\"url\\":\\"https://example.com/docs\\"}"}\n\n'),
  );
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_delta\ndata: {"content":" Next I should read the page."}\n\n'),
  );

  expect(screen.getByText("Searching current sources")).toBeInTheDocument();
  expect(screen.queryByText("Thinking")).not.toBeInTheDocument();
});

test("keeps Thinking when pre-tool preamble streams before a pending tool call", async () => {
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

  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_delta\ndata: {"content":"I should search current sources."}\n\n'),
  );
  // The model emits preamble text and then signals a pending tool call. Despite
  // the streamed text, the label must stay "Thinking" — the answer phase has not
  // begun, a tool is about to run.
  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Let me search."}\n\n'));
  streamController.current?.enqueue(new TextEncoder().encode("event: tool_pending\ndata: {}\n\n"));

  expect(await screen.findByText("Let me search.")).toBeInTheDocument();
  expect(screen.getByText("Thinking")).toBeInTheDocument();
  expect(screen.queryByText("Search current sources")).not.toBeInTheDocument();
});

test("keeps active activity trace visible while assistant text is streaming", async () => {
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

  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_delta\ndata: {"content":"I checked the source first."}\n\n'),
  );
  const trace = await screen.findByRole("status", { name: /slopr activity trace/i });
  // The window auto-opens while reasoning streams — the body is shown without a click.
  expect(await screen.findByText("I checked the source first.")).toBeInTheDocument();
  expect(within(trace).getByRole("button", { name: /hide activity/i })).toBeInTheDocument();

  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Hel"}\n\n'));

  expect(await screen.findByText("Hel")).toBeInTheDocument();
  // No reasoning title has arrived yet, so the window stays open while the answer streams.
  expect(within(trace).getByText("I checked the source first.")).toBeInTheDocument();
  expect(within(trace).getByRole("button", { name: /hide activity/i })).toBeInTheDocument();
  // Reasoning rows use the clock node — no per-row completion checkmark mid-stream.
  expect(document.querySelector(".ui-activity-trace-icon-reasoning-complete")).toBeNull();

  streamController.current?.enqueue(
    new TextEncoder().encode(
      'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Hello.","reasoningContent":"I checked the source first.","createdAt":"2026-05-30T00:00:01Z"}\n\n',
    ),
  );
  streamController.current?.enqueue(new TextEncoder().encode("event: done\ndata: {}\n\n"));
  streamController.current?.close();

  expect(await screen.findByText("Hello.")).toBeInTheDocument();
  // The result is in: the thinking window collapses itself once the turn completes.
  const completedToggle = await screen.findByRole("button", { name: /show activity/i });
  await waitFor(() => expect(screen.queryByText("I checked the source first.")).not.toBeInTheDocument());
  // Expanding the completed trace still reveals the clock timeline node and the Done cap.
  fireEvent.click(completedToggle);
  expect(screen.getByText("I checked the source first.")).toBeInTheDocument();
  expect(document.querySelector(".ui-activity-clock-icon")).not.toBeNull();
  expect(screen.getByText("Done")).toBeInTheDocument();
});

test("hides the copy action until the assistant answer finishes streaming", async () => {
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

  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Partial"}\n\n'),
  );

  expect(await screen.findByText("Partial")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Copy response" })).not.toBeInTheDocument();

  streamController.current?.enqueue(
    new TextEncoder().encode(
      'event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Partial answer.","createdAt":"2026-05-30T00:00:01Z"}\n\n',
    ),
  );
  streamController.current?.enqueue(new TextEncoder().encode("event: done\ndata: {}\n\n"));
  streamController.current?.close();

  expect(await screen.findByText("Partial answer.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Copy response" })).toBeInTheDocument();
});

test("centers reasoning activity dots inside their row circles", () => {
  const css = readFileSync("src/index.css", "utf8");
  const reasoningIconRule =
    css.match(/\.ui-activity-trace-icon-reasoning\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";
  const reasoningDotRule =
    css.match(/\.ui-activity-trace-icon-reasoning::after\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";
  const reasoningParagraphRule =
    css.match(/\.ui-activity-reasoning p\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";

  expect(reasoningIconRule).toContain("border: 1px solid currentColor");
  expect(reasoningDotRule).toContain("display: block");
  expect(reasoningDotRule).toContain("width: 0.25rem");
  expect(reasoningDotRule).toContain("height: 0.25rem");
  expect(reasoningParagraphRule).toContain("margin: 0");
  expect(reasoningParagraphRule).toContain("transform: translateY(1px)");
  expect(css).toContain(".ui-activity-trace-icon-reasoning-complete::after");
  expect(css).toContain("border-bottom: 1.5px solid currentColor");
  expect(css).toContain("border-left: 1.5px solid currentColor");
});

test("spaces activity trace connector lines away from adjacent icons", () => {
  const css = readFileSync("src/index.css", "utf8");
  const iconRule = css.match(/\.ui-activity-trace-icon\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";
  const reasoningIconRule =
    css.match(/\.ui-activity-trace-icon-reasoning\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";
  const connectorRule =
    css.match(/\.ui-activity-trace-row:not\(:last-child\)::before\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";

  expect(css).toContain("--ui-activity-icon-offset: 0.09rem");
  expect(iconRule).toContain("margin-top: var(--ui-activity-icon-offset)");
  expect(iconRule).not.toContain("border: 1px solid currentColor");
  expect(iconRule).not.toContain("border-radius: 9999px");
  expect(reasoningIconRule).toContain("border-radius: 9999px");
  expect(reasoningIconRule).not.toContain("margin-top:");
  expect(connectorRule).toContain("top: calc(0.25rem + var(--ui-activity-icon-offset) + 1rem + 0.25rem)");
  expect(connectorRule).toContain("bottom: -0.225rem");
  expect(connectorRule).toContain("left: calc(0.5rem - 0.5px)");
});

test("keeps existing search activity icon glyph design", () => {
  const source = readFileSync("src/chat/ActivityTracePanel.tsx", "utf8");
  const css = readFileSync("src/index.css", "utf8");
  const globeIcon = source.match(/function GlobeTraceIcon\(\) \{(?<body>[\s\S]*?)\n\}/)?.groups?.body ?? "";
  const globeIconRule = css.match(/\.ui-activity-globe-icon\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";
  const fetchFaviconRule = css.match(/\.ui-activity-fetch-icon-favicon\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";
  const toolHeaderRule = css.match(/\.ui-activity-tool-header\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";
  const resultListRule = css.match(/\.ui-activity-result-list\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";

  // The search node now renders the Anthropicons globe glyph via <Icon> instead
  // of a hand-drawn SVG; the .ui-activity-globe-icon sizing rule is preserved.
  expect(globeIcon).toContain('name="globe"');
  expect(globeIconRule).toContain("width: 1.125rem !important");
  expect(globeIconRule).toContain("height: 1.125rem !important");
  expect(source).not.toContain("ui-activity-trace-icon ui-activity-trace-icon-arrow");
  expect(source).toContain("ui-activity-trace-row-reasoning");
  expect(source).toContain("ui-activity-trace-row-tool");
  expect(toolHeaderRule).toContain("transform: translateY(-1px)");
  expect(fetchFaviconRule).toContain("width: 1.125rem");
  expect(fetchFaviconRule).toContain("height: 1.125rem");
  expect(resultListRule).toContain("max-height: 12rem");
  expect(source).not.toContain("ui-activity-trace-chevron-icon");
});

test("keeps generated activity title sweep visually aligned with Thinking", () => {
  const css = readFileSync("src/index.css", "utf8");
  const thinkingRule = css.match(/\.ui-thinking-label-active\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";
  const generatedTitleRule =
    css.match(/\.ui-activity-tool-title\.ui-thinking-label-active\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";

  expect(thinkingRule).toContain("color: #8f8a81");
  expect(thinkingRule).toContain("font-weight: 500");
  expect(generatedTitleRule).toContain("color: #8f8a81");
  expect(generatedTitleRule).toContain("font-weight: 500");
  expect(css).not.toContain("ui-activity-tool-title-sweeping");
});

test("keeps the reasoning clamp height in sync with REASONING_CAP_PX", () => {
  const source = readFileSync("src/chat/ActivityTracePanel.tsx", "utf8");
  const css = readFileSync("src/index.css", "utf8");
  const capPx = Number(source.match(/REASONING_CAP_PX\s*=\s*(?<px>\d+)/)?.groups?.px);
  const clampRule = css.match(/\.ui-activity-reasoning-clamp\s*\{(?<body>[^}]*)\}/)?.groups?.body ?? "";
  const maxHeightRem = Number(clampRule.match(/max-height:\s*(?<rem>[\d.]+)rem/)?.groups?.rem);

  // The JS clamp threshold (px) and the CSS max-height (rem) must describe the
  // same height, or the overflow measurement desyncs from the visual clamp/fade.
  expect(capPx).toBe(192);
  expect(maxHeightRem * 16).toBe(capPx);
});

test("shows active activity trace with reasoning and tool activity before assistant output", async () => {
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

  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_reasoning_delta\ndata: {"content":"I should search current sources."}\n\n'),
  );
  streamController.current?.enqueue(
    new TextEncoder().encode('event: tool_call\ndata: {"id":"call_1","name":"search__web","arguments":"{\\"query\\":\\"agentgateway kgateway\\"}"}\n\n'),
  );

  const trace = await screen.findByRole("status", { name: /slopr activity trace/i });
  expect(within(trace).getByText("Thinking")).toBeInTheDocument();
  // The window auto-opens while inference runs — no click needed.
  expect(within(trace).getByRole("button", { name: /hide activity/i })).toBeInTheDocument();
  expect(within(trace).getByText("I should search current sources.")).toBeInTheDocument();
  expect(within(trace).getByText("agentgateway kgateway")).toBeInTheDocument();
  expect(within(trace).getByText("Running")).toBeInTheDocument();
  // While the turn is still active, the timeline is not yet capped with a "Done" node.
  expect(within(trace).queryByText("Done")).toBeNull();
});

test("hides empty activity trace when the stream fails", async () => {
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

  expect(await screen.findByRole("status", { name: /slopr activity trace/i })).toBeInTheDocument();

  streamController.current?.enqueue(new TextEncoder().encode('event: error\ndata: {"error":"llm is not configured"}\n\n'));
  streamController.current?.close();

  expect(await screen.findByText("llm is not configured")).toBeInTheDocument();
  expect(screen.queryByRole("status", { name: /slopr activity trace/i })).not.toBeInTheDocument();
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
      return Response.json({ items: [
        { id: "t1", title: "Existing chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" },
      ], nextCursor: null });
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
  messages: Array<{
    id: string;
    role: "assistant" | "user";
    content: string;
    activityTrace?: unknown[];
    artifacts?: Array<{
      id: string;
      displayFilename: string;
      mimeType: string;
      sizeBytes: number;
      downloadUrl: string;
    }>;
  }> = [],
) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") {
      return Response.json({ items: [
        { id: "t1", title: "Existing chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" },
      ], nextCursor: null });
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

function stoppingChatFetch() {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
    if (url === "/api/projects") return Response.json([]);
    if (url === "/api/threads?limit=30") return Response.json({ items: [threadFixture()], nextCursor: null });
    if (url === "/api/threads/t1") return Response.json({ thread: threadFixture(), messages: [] });
    if (url === "/api/threads/t1/messages:stop" && init?.method === "POST") {
      return new Response("", { status: 204 });
    }
    if (url === "/api/threads/t1/messages:stream" && init?.method === "POST") {
      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(
            new TextEncoder().encode(
              'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n',
            ),
          );
          init.signal?.addEventListener("abort", () => {
            controller.error(new DOMException("Aborted", "AbortError"));
          });
        },
      });
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
      return Response.json({ items: [
        { id: "t1", title: "Existing chat", starred: false, createdAt: "2026-05-30T00:00:00Z", updatedAt: "2026-05-30T00:00:00Z" },
      ], nextCursor: null });
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
        'event: mcp_status\ndata: {"active":2,"configured":3,"servers":[{"name":"fetch","active":true},{"name":"obscura","active":false},{"name":"tavily","active":true}]}\n\n' +
        "event: done\ndata: {}\n\n",
    ),
  );

  await sendMessageInExistingChat();

  const indicator = await screen.findByTitle("2 of 3 MCP servers active. Failed: obscura");
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

test("loads MCP status into the chat header", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") return Response.json({ items: [threadFixture()], nextCursor: null });
      if (url === "/api/threads/t1") return Response.json({ thread: threadFixture(), messages: [] });
      if (url === "/api/mcp/status") return Response.json({ active: 1, configured: 2 });
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const header = await screen.findByRole("banner", { name: "Chat header" });
  const indicator = within(header).getByTitle("1 of 2 MCP servers active");
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
  expect(document.querySelector(".ui-markdown div")).toBeNull();
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

  expect(transcript.querySelector(".ui-chat-rail")).toBeInTheDocument();
  expect(composerDock.querySelector(".ui-chat-rail")).toBeInTheDocument();
  expect(transcript.querySelector(".ui-user-message")).toHaveClass("ml-auto");
  expect(transcript.querySelector(".ui-assistant-message")).toBeInTheDocument();
});

test("anchors the chat send button inside the composer action area", async () => {
  vi.stubGlobal("fetch", persistedMarkdownChatFetch());

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));

  const sendButton = screen.getByRole("button", { name: "Send message" });

  expect(sendButton.closest("form")).toHaveClass("ui-composer");
  expect(sendButton).toHaveClass("ui-composer-send");
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
        element.classList.contains("ui-markdown") &&
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
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const encoder = new TextEncoder();
  const content = "```html\n<!doctype html>\n<html><body><h1>Report</h1></body></html>\n```";
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
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
  expect(screen.getByText(/Receiving file\.\.\. \d+\.\d KB received/)).toBeInTheDocument();
  expect(screen.queryByText(/doctype html/i)).not.toBeInTheDocument();

  streamController.current?.enqueue(
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
  streamController.current?.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
  streamController.current?.close();

  expect(await screen.findByRole("button", { name: "Download HTML response" })).toBeInTheDocument();
  expect(screen.queryByText(/Receiving file/)).not.toBeInTheDocument();
  expect(screen.queryByText(/doctype html/i)).not.toBeInTheDocument();
});

test("shows received KB while a fenced HTML artifact is streaming", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const encoder = new TextEncoder();
  const htmlChunk = "<!doctype html>\n" + "a".repeat(1536);
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
      controller.enqueue(
        encoder.encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
      controller.enqueue(
        encoder.encode(`event: assistant_delta\ndata: ${JSON.stringify({ content: `\`\`\`html\n${htmlChunk}` })}\n\n`),
      );
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  await sendMessageInExistingChat();

  expect(await screen.findByText("HTML response")).toBeInTheDocument();
  expect(screen.getByText("Receiving file... 1.5 KB received")).toBeInTheDocument();

  streamController.current?.close();
});

test("shows received KB while a fenced PDF artifact is streaming", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const encoder = new TextEncoder();
  const pdfChunk = "%PDF-1.7\n" + "a".repeat(1536);
  const content = `\`\`\`pdf\n${pdfChunk}\n\`\`\``;
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
      controller.enqueue(
        encoder.encode(
          'event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n',
        ),
      );
      controller.enqueue(
        encoder.encode(`event: assistant_delta\ndata: ${JSON.stringify({ content: `\`\`\`pdf\n${pdfChunk}` })}\n\n`),
      );
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  await sendMessageInExistingChat();

  expect(await screen.findByText("PDF response")).toBeInTheDocument();
  expect(screen.getByText("Receiving file... 1.5 KB received")).toBeInTheDocument();
  expect(screen.queryByText(/%PDF-1\.7/)).not.toBeInTheDocument();

  streamController.current?.enqueue(
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
  streamController.current?.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
  streamController.current?.close();

  expect(await screen.findByRole("button", { name: "Download PDF response" })).toBeInTheDocument();
  expect(screen.queryByText(/Receiving file/)).not.toBeInTheDocument();
  expect(screen.queryByText(/%PDF-1\.7/)).not.toBeInTheDocument();
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

test("renders small fenced YAML inline instead of as a download", async () => {
  const content = "```yaml\nname: slopr\nmode: local\n```";
  vi.stubGlobal("fetch", mcpStreamFetch(assistantEventForContent(content) + "event: done\ndata: {}\n\n"));

  await sendMessageInExistingChat();

  expect(
    await screen.findByText(
      (_, element) =>
        element !== null &&
        element.tagName.toLowerCase() === "code" &&
        element.textContent?.includes("name: slopr") === true,
    ),
  ).toBeInTheDocument();
  expect(screen.queryByText("YAML response")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Download YAML response" })).not.toBeInTheDocument();
});

test("downloads fenced generated data without markdown fences", async () => {
  const objectURL = "blob:ui-response";
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
        return Response.json({ items: [
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
        ], nextCursor: null });
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

test("keeps a running thread stream alive while browsing another chat", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  let streamSignalAborted = false;
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
        return Response.json({ items: [
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
        ], nextCursor: null });
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
        init.signal?.addEventListener("abort", () => {
          streamSignalAborted = true;
        });
        return new Response(stream, { status: 200 });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "First chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));
  await screen.findByRole("button", { name: "Stop response" });
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Already started"}\n\n'),
  );
  expect(await screen.findByText("Already started")).toBeInTheDocument();

  fireEvent.click(await screen.findByRole("button", { name: "Second chat" }));

  expect(await screen.findByRole("heading", { name: "Second chat" })).toBeInTheDocument();
  expect(streamSignalAborted).toBe(false);
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_delta\ndata: {"content":" while away"}\n\n'),
  );
  expect(screen.queryByText("Already started while away")).not.toBeInTheDocument();

  fireEvent.click(await screen.findByRole("button", { name: "First chat" }));
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_delta\ndata: {"content":" and back"}\n\n'),
  );

  // Streaming-Text wird beim Rendern in Segment-<span>s aufgeteilt (Fade-in),
  // daher matcht ein Single-Node-findByText nicht mehr. Wir suchen das tiefste
  // Element, dessen gesamter Textinhalt dem erwarteten String entspricht.
  const fullStreamText = "Already started while away and back";
  expect(
    await screen.findByText((_content, element) => {
      if (element?.textContent !== fullStreamText) return false;
      return Array.from(element.children).every(
        (child) => child.textContent !== fullStreamText,
      );
    }),
  ).toBeInTheDocument();
});

test("blocks starting a second chat stream while another thread is running", async () => {
  const stream = new ReadableStream<Uint8Array>({ start() {} });
  let secondStreamRequests = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") {
        return Response.json({ items: [
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
        ], nextCursor: null });
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
      if (url === "/api/threads/t2/messages:stream" && init?.method === "POST") {
        secondStreamRequests += 1;
        return new Response("", { status: 500 });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "First chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));
  await screen.findByRole("button", { name: "Stop response" });

  fireEvent.click(await screen.findByRole("button", { name: "Second chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Second" } });

  expect(screen.getByRole("button", { name: "Send message" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Send message" }));

  expect(secondStreamRequests).toBe(0);
});

test("does not stop a background stream with Escape while another chat is open", async () => {
  const stream = new ReadableStream<Uint8Array>({ start() {} });
  let stopRequests = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/api/me") return Response.json({ id: "u1", username: "jan", role: "user" });
      if (url === "/api/projects") return Response.json([]);
      if (url === "/api/threads?limit=30") {
        return Response.json({ items: [
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
        ], nextCursor: null });
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
      if (url === "/api/threads/t1/messages:stop" && init?.method === "POST") {
        stopRequests += 1;
        return new Response("", { status: 204 });
      }
      throw new Error(`unexpected fetch ${url}`);
    }),
  );

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "First chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));
  await screen.findByRole("button", { name: "Stop response" });

  fireEvent.click(await screen.findByRole("button", { name: "Second chat" }));
  expect(await screen.findByRole("heading", { name: "Second chat" })).toBeInTheDocument();
  fireEvent.keyDown(window, { key: "Escape" });

  expect(stopRequests).toBe(0);
});

test("surfaces the server error and keeps failed activity trace visible", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: tool_call\ndata: {"id":"call_1","name":"search__web","arguments":"{\\"query\\":\\"agentgateway\\"}"}\n\n'));
      controller.enqueue(encoder.encode('event: tool_result\ndata: {"id":"call_1","name":"search__web","content":"tool failed: timeout"}\n\n'));
      controller.enqueue(encoder.encode('event: error\ndata: {"error":"llm is not configured"}\n\n'));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("llm is not configured")).toBeInTheDocument();
  expect(screen.getByPlaceholderText(/message/i)).toHaveValue("Hi");
  const traceToggle = screen.getByRole("button", { name: /activity/i });
  const trace = traceToggle.closest(".ui-activity-trace");
  expect(trace).not.toBeNull();
  expect(trace?.querySelector(".ui-thinking-label-active")).toBeNull();
  // The turn has ended (the request failed), so the trace collapses itself. Wait
  // for that collapse — isSending flips and the collapse effect runs after the
  // error text renders — then open it to inspect the failed timeline.
  if (traceToggle.getAttribute("aria-expanded") === "false") {
    fireEvent.click(traceToggle);
  }
  expect(within(trace as HTMLElement).getByRole("button", { name: /hide activity/i })).toBeInTheDocument();
  expect(within(trace as HTMLElement).getByText("agentgateway")).toBeInTheDocument();
  expect(within(trace as HTMLElement).getByText("Failed")).toBeInTheDocument();
});

test("renders unknown tool calls with safe fallback details", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: tool_call\ndata: {"id":"call_1","name":"custom__lookup","arguments":"not-json"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Done.","createdAt":"2026-05-30T00:00:01Z"}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("Done.")).toBeInTheDocument();
  const toggle = screen.getByRole("button", { name: /show activity/i });
  expect(toggle).toBeInTheDocument();
  expect(screen.queryByText("custom lookup")).not.toBeInTheDocument();

  fireEvent.click(toggle);

  expect(await screen.findByText("custom lookup")).toBeInTheDocument();
  // Both the tool status pill and the terminal timeline node read "Done".
  const doneNodes = screen.getAllByText("Done");
  expect(doneNodes.some((node) => node.classList.contains("ui-activity-status-pill"))).toBe(true);
  expect(doneNodes.some((node) => node.classList.contains("ui-activity-done-label"))).toBe(true);
});

test("renders fetch tool rows with a timeline favicon and a clickable URL", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: tool_call\ndata: {"id":"call_1","name":"fetch__fetch","arguments":"{\\"url\\":\\"https://www.getmaxim.ai/bifrost/resources/governance\\"}"}\n\n'));
      controller.enqueue(encoder.encode('event: tool_result\ndata: {"id":"call_1","name":"fetch__fetch","content":"Page content"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Done.","createdAt":"2026-05-30T00:00:01Z"}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("Done.")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: /show activity/i }));

  // The fetch timeline node uses the site's favicon instead of the old arrow glyph.
  expect(await screen.findByText("getmaxim.ai")).toBeInTheDocument();
  const favicon = document.querySelector(".ui-activity-fetch-icon-favicon");
  expect(favicon).toHaveAttribute("src", "https://www.google.com/s2/favicons?domain=getmaxim.ai&sz=32");
  expect(document.querySelector(".ui-activity-trace-icon-arrow")).toBeNull();
  expect(document.querySelector(".ui-activity-tool-favicon")).toBeNull();
  // The full URL is a link that opens in a new tab — no redundant result frame.
  const link = screen.getByRole("link", { name: "https://www.getmaxim.ai/bifrost/resources/governance" });
  expect(link).toHaveAttribute("href", "https://www.getmaxim.ai/bifrost/resources/governance");
  expect(link).toHaveAttribute("target", "_blank");
  expect(link).toHaveAttribute("rel", "noreferrer");
  expect(document.querySelector(".ui-activity-result-list")).toBeNull();
});

test("opens schemeless fetch tool URLs as remote links", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: tool_call\ndata: {"id":"call_1","name":"fetch__fetch","arguments":"{\\"url\\":\\"www.getmaxim.ai/bifrost/resources/governance\\"}"}\n\n'));
      controller.enqueue(encoder.encode('event: tool_result\ndata: {"id":"call_1","name":"fetch__fetch","content":"Page content"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Done.","createdAt":"2026-05-30T00:00:01Z"}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("Done.")).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: /show activity/i }));

  const link = await screen.findByRole("link", { name: "www.getmaxim.ai/bifrost/resources/governance" });
  expect(link).toHaveAttribute("href", "https://www.getmaxim.ai/bifrost/resources/governance");
  expect(link).toHaveAttribute("target", "_blank");
  expect(link).toHaveAttribute("rel", "noreferrer");
});

test("does not repeat the collapsed headline as the reasoning row title", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: assistant_reasoning_delta\ndata: {"content":"The user is asking about Einstein."}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_reasoning_title\ndata: {"id":"reasoning-1","title":"Summarizing Einstein\'s life and contributions"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Albert Einstein was a physicist.","createdAt":"2026-05-30T00:00:01Z"}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Tell me about einstein" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  expect(await screen.findByText("Albert Einstein was a physicist.")).toBeInTheDocument();
  const toggle = screen.getByRole("button", { name: /show activity/i });
  expect(toggle).toHaveTextContent("Summarizing Einstein's life and contributions");
  fireEvent.click(toggle);

  // The headline appears once (the toggle); the reasoning row drops the duplicate.
  expect(await screen.findByText("The user is asking about Einstein.")).toBeInTheDocument();
  expect(screen.getAllByText("Summarizing Einstein's life and contributions")).toHaveLength(1);
  // The reasoning row shows the clock timeline node; the turn ends with a Done node.
  expect(document.querySelector(".ui-activity-clock-icon")).not.toBeNull();
  expect(screen.getByText("Done")).toBeInTheDocument();
  expect(document.querySelector(".ui-thinking-status-active, .ui-thinking-status-complete")).toBeNull();
});

test("reveals the message action icons only after the answer settles", async () => {
  const streamController: { current?: ReadableStreamDefaultController<Uint8Array> } = {};
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      streamController.current = controller;
      controller.enqueue(
        new TextEncoder().encode('event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Hi","createdAt":"2026-05-30T00:00:00Z"}\n\n'),
      );
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), { target: { value: "Hi" } });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  // While the answer streams, none of the action icons are rendered.
  streamController.current?.enqueue(new TextEncoder().encode('event: assistant_delta\ndata: {"content":"Here is the answer."}\n\n'));
  expect(await screen.findByText("Here is the answer.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /read aloud/i })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /copy response/i })).not.toBeInTheDocument();

  // Once the message settles they appear together with the metrics footer.
  streamController.current?.enqueue(
    new TextEncoder().encode('event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"Here is the answer.","createdAt":"2026-05-30T00:00:01Z"}\n\n'),
  );
  streamController.current?.enqueue(new TextEncoder().encode("event: done\ndata: {}\n\n"));
  streamController.current?.close();

  expect(await screen.findByRole("button", { name: /read aloud/i })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /copy response/i })).toBeInTheDocument();
});

test("keeps just-completed activity trace collapsed before the assistant answer until opened", async () => {
  const stream = new ReadableStream({
    start(controller) {
      const encoder = new TextEncoder();
      controller.enqueue(encoder.encode('event: user_message\ndata: {"id":"m1","threadId":"t1","role":"user","content":"Search for updates","createdAt":"2026-05-30T00:00:00Z"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_reasoning_delta\ndata: {"content":"I should search current sources."}\n\n'));
      controller.enqueue(encoder.encode('event: tool_call\ndata: {"id":"call_1","name":"search__web","arguments":"{\\"query\\":\\"agentgateway kgateway\\"}"}\n\n'));
      controller.enqueue(encoder.encode('event: tool_result\ndata: {"id":"call_1","name":"search__web","content":"{\\"results\\":[{\\"title\\":\\"Agentgateway\\",\\"url\\":\\"https://agentgateway.dev\\",\\"snippet\\":\\"**Next generation proxy**\\"},{\\"title\\":\\"# Our Story and Lumon Brand\\",\\"url\\":\\"lumon.com/story\\"},{\\"title\\":\\"Malformed source\\",\\"url\\":\\"not a url\\"}]}"}\n\n'));
      controller.enqueue(encoder.encode('event: assistant_message\ndata: {"id":"m2","threadId":"t1","role":"assistant","content":"I found the update.","createdAt":"2026-05-30T00:00:01Z"}\n\n'));
      controller.enqueue(encoder.encode("event: done\ndata: {}\n\n"));
      controller.close();
    },
  });
  vi.stubGlobal("fetch", chatThreadFetch(stream));

  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Existing chat" }));
  fireEvent.change(await screen.findByPlaceholderText(/message/i), {
    target: { value: "Search for updates" },
  });
  fireEvent.click(screen.getByRole("button", { name: /send/i }));

  const answer = await screen.findByText("I found the update.");
  const toggle = screen.getByRole("button", { name: /show activity/i });
  expect(toggle).toHaveTextContent("Search current sources");
  expect(screen.queryByRole("status", { name: /slopr activity trace/i })).not.toBeInTheDocument();
  expect(screen.queryByText("I should search current sources.")).not.toBeInTheDocument();
  expect(screen.queryByText("agentgateway kgateway")).not.toBeInTheDocument();

  fireEvent.click(toggle);

  expect(await screen.findByText("I should search current sources.")).toBeInTheDocument();
  expect(screen.getByText("agentgateway kgateway")).toBeInTheDocument();
  // Tools make this a real timeline: the turn is capped with the Done node glyph.
  expect(document.querySelector(".ui-activity-trace-icon-done")).not.toBeNull();
  expect(document.querySelector(".ui-activity-trace-body-flat")).toBeNull();
  const agentgatewayLink = screen.getByRole("link", { name: /Agentgateway/ });
  expect(agentgatewayLink).toHaveAttribute("href", "https://agentgateway.dev/");
  expect(agentgatewayLink).toHaveAttribute("target", "_blank");
  expect(agentgatewayLink).toHaveAttribute("rel", "noreferrer");
  expect(screen.queryByText("Next generation proxy")).not.toBeInTheDocument();
  expect(screen.queryByText("**Next generation proxy**")).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: /Our Story and Lumon Brand/ })).toHaveAttribute("href", "https://lumon.com/story");
  expect(screen.queryByText("# Our Story and Lumon Brand")).not.toBeInTheDocument();
  expect(screen.getByText("Malformed source")).toBeInTheDocument();
  const resultList = document.querySelector(".ui-activity-result-list");
  const faviconImages = resultList?.querySelectorAll("img.ui-activity-favicon");
  expect(faviconImages).toHaveLength(2);
  expect(faviconImages?.[0]).toHaveAttribute(
    "src",
    "https://www.google.com/s2/favicons?domain=agentgateway.dev&sz=32",
  );
  expect(faviconImages?.[1]).toHaveAttribute(
    "src",
    "https://www.google.com/s2/favicons?domain=lumon.com&sz=32",
  );
  expect(toggle.compareDocumentPosition(answer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
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
    if (url === "/api/threads?limit=30") return Response.json({ items: [], nextCursor: null });
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

function projectFixture() {
  return {
    id: "p1",
    name: "Research",
    description: "Project notes",
    createdAt: "2026-05-30T00:00:00Z",
    updatedAt: "2026-05-31T00:00:00Z",
  };
}

function greetingPattern(name: string) {
  return new RegExp(`^((Morning|Afternoon|Evening), ${name}|Up late, ${name}\\?|${name} returns!)$`);
}
