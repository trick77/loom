import { afterEach, describe, expect, test, vi } from "vitest";
import { AuthExpiredError } from "./http";
import {
  bulkDeleteThreads,
  createThread,
  deleteThread,
  getThread,
  listThreadIds,
  listThreads,
  searchThreadContent,
  setThreadStarred,
  stopMessage,
  updateThread,
} from "./threads";

afterEach(() => {
  vi.unstubAllGlobals();
});

const thread = {
  id: "t1",
  title: "Chat",
  starred: false,
  createdAt: "2026-06-01T00:00:00Z",
  updatedAt: "2026-06-01T00:00:00Z",
};

function stubFetch(response: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

// stubFetchEach builds a fresh Response per call — a single Response instance
// can only be read once, so tests that call the API twice need this.
function stubFetchEach(build: () => unknown) {
  const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(build()));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("listThreads", () => {
  test("omits the query string when no params are given", async () => {
    const fetchMock = stubFetch(
      Response.json({ items: [thread], nextCursor: null }),
    );

    await expect(listThreads()).resolves.toEqual({
      items: [thread],
      nextCursor: null,
    });
    expect(fetchMock).toHaveBeenCalledWith("/api/threads");
  });

  test("sends the literal 'null' sentinel for unassigned threads", async () => {
    const fetchMock = stubFetch(Response.json({ items: [] }));

    await listThreads({ projectId: null });

    expect(fetchMock).toHaveBeenCalledWith("/api/threads?projectId=null");
  });

  test("sends the project id when assigned", async () => {
    const fetchMock = stubFetch(Response.json({ items: [] }));

    await listThreads({ projectId: "p1" });

    expect(fetchMock).toHaveBeenCalledWith("/api/threads?projectId=p1");
  });

  test("builds the full query for every parameter", async () => {
    const fetchMock = stubFetch(Response.json({ items: [] }));

    await listThreads({
      projectId: "p1",
      starred: false,
      archived: true,
      search: "vpn setup",
      limit: 25,
      cursor: "c1",
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads?projectId=p1&starred=false&archived=true&search=vpn+setup&limit=25&cursor=c1",
    );
  });

  test("drops an empty search and a null or empty cursor", async () => {
    const fetchMock = stubFetchEach(() => Response.json({ items: [] }));

    await listThreads({ search: "", cursor: null });
    expect(fetchMock).toHaveBeenCalledWith("/api/threads");

    await listThreads({ search: "", cursor: "" });
    expect(fetchMock).toHaveBeenLastCalledWith("/api/threads");
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(listThreads()).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(listThreads()).rejects.toThrow("failed to load threads");
  });
});

describe("listThreadIds", () => {
  test("requests every id when no search is given", async () => {
    const fetchMock = stubFetch(Response.json(["t1", "t2"]));

    await expect(listThreadIds()).resolves.toEqual(["t1", "t2"]);
    expect(fetchMock).toHaveBeenCalledWith("/api/threads/ids");
  });

  test("passes the search term and ignores an empty one", async () => {
    const fetchMock = stubFetchEach(() => Response.json([]));

    await listThreadIds({ search: "vpn setup" });
    expect(fetchMock).toHaveBeenCalledWith("/api/threads/ids?search=vpn+setup");

    await listThreadIds({ search: "" });
    expect(fetchMock).toHaveBeenLastCalledWith("/api/threads/ids");
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(listThreadIds()).rejects.toThrow("failed to load thread ids");
  });
});

describe("searchThreadContent", () => {
  test("splits the snippet out of each returned thread", async () => {
    const fetchMock = stubFetch(
      Response.json({ items: [{ ...thread, snippet: "…vpn…" }] }),
    );

    await expect(searchThreadContent({ query: "vpn" })).resolves.toEqual([
      { thread, snippet: "…vpn…" },
    ]);
    expect(fetchMock).toHaveBeenCalledWith("/api/threads/search?q=vpn");
  });

  test("includes the limit and project id filters", async () => {
    const fetchMock = stubFetch(Response.json({ items: [] }));

    await searchThreadContent({ query: "vp n", limit: 5, projectId: "p1" });

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/search?q=vp+n&limit=5&projectId=p1",
    );
  });

  test.each([
    ["null", null],
    ["empty", ""],
  ])("omits a %s project id", async (_label, projectId) => {
    const fetchMock = stubFetch(Response.json({ items: [] }));

    await searchThreadContent({ query: "vpn", projectId });

    expect(fetchMock).toHaveBeenCalledWith("/api/threads/search?q=vpn");
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(searchThreadContent({ query: "vpn" })).rejects.toThrow(
      "failed to search threads",
    );
  });
});

describe("createThread", () => {
  test("posts an empty payload by default", async () => {
    const fetchMock = stubFetch(Response.json(thread));

    await expect(createThread()).resolves.toEqual(thread);
    expect(fetchMock).toHaveBeenCalledWith("/api/threads", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });
  });

  test("posts the project id and title", async () => {
    const fetchMock = stubFetch(Response.json(thread));

    await createThread({ projectId: "p1", title: "Chat" });

    expect(fetchMock).toHaveBeenCalledWith("/api/threads", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ projectId: "p1", title: "Chat" }),
    });
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(createThread()).rejects.toThrow("failed to create thread");
  });
});

describe("getThread", () => {
  test("reads the thread and encodes the id", async () => {
    const fetchMock = stubFetch(Response.json({ thread, messages: [] }));

    await expect(getThread("t 1")).resolves.toEqual({ thread, messages: [] });
    expect(fetchMock).toHaveBeenCalledWith("/api/threads/t%201");
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(getThread("t1")).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 404 }));

    await expect(getThread("t1")).rejects.toThrow("failed to load thread");
  });
});

describe("setThreadStarred", () => {
  test.each([
    [true, "star"],
    [false, "unstar"],
  ])("posts to the %s endpoint", async (starred, action) => {
    const fetchMock = stubFetch(Response.json({ ...thread, starred }));

    await expect(setThreadStarred("t1", starred)).resolves.toEqual({
      ...thread,
      starred,
    });
    expect(fetchMock).toHaveBeenCalledWith(`/api/threads/t1/${action}`, {
      method: "POST",
    });
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(setThreadStarred("t1", true)).rejects.toThrow(
      "failed to update thread",
    );
  });
});

describe("updateThread", () => {
  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(updateThread("t1", { title: "x" })).rejects.toBeInstanceOf(
      AuthExpiredError,
    );
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(updateThread("t1", { title: "x" })).rejects.toThrow(
      "failed to update thread",
    );
  });
});

describe("deleteThread", () => {
  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(deleteThread("t1")).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 409 }));

    await expect(deleteThread("t1")).rejects.toThrow("failed to delete thread");
  });
});

describe("bulkDeleteThreads", () => {
  test("posts the thread ids and returns the deleted count", async () => {
    const fetchMock = stubFetch(Response.json({ deleted: 2 }));

    await expect(bulkDeleteThreads(["t1", "t2"])).resolves.toEqual({
      deleted: 2,
    });
    expect(fetchMock).toHaveBeenCalledWith("/api/threads:delete", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ threadIds: ["t1", "t2"] }),
    });
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(bulkDeleteThreads(["t1"])).rejects.toThrow(
      "failed to delete threads",
    );
  });
});

describe("stopMessage", () => {
  test("posts without a source query when none is given", async () => {
    const fetchMock = stubFetch(new Response(null, { status: 204 }));

    await expect(stopMessage("t1")).resolves.toBeUndefined();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t1/messages:stop",
      expect.objectContaining({ method: "POST" }),
    );
  });

  test("encodes the stop source", async () => {
    const fetchMock = stubFetch(new Response(null, { status: 204 }));

    await stopMessage("t1", "stop button");

    expect(fetchMock).toHaveBeenCalledWith(
      "/api/threads/t1/messages:stop?source=stop%20button",
      expect.objectContaining({ method: "POST", signal: expect.anything() }),
    );
  });

  test("throws AuthExpiredError on 401", async () => {
    stubFetch(new Response("", { status: 401 }));

    await expect(stopMessage("t1")).rejects.toBeInstanceOf(AuthExpiredError);
  });

  test("throws on a non-ok response", async () => {
    stubFetch(new Response("", { status: 500 }));

    await expect(stopMessage("t1")).rejects.toThrow("failed to stop message");
  });
});
