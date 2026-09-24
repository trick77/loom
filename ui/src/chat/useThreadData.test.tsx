import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";

import { useThreadData } from "./useThreadData";

const api = vi.hoisted(() => ({
  getThread: vi.fn(),
  listProjects: vi.fn(),
  listThreads: vi.fn(),
}));

vi.mock("../api", async () => {
  const actual = await vi.importActual<typeof import("../api")>("../api");
  return { ...actual, ...api };
});

function thread(id: string) {
  return {
    id,
    title: `Thread ${id}`,
    starred: false,
    createdAt: "2026-05-30T00:00:00Z",
    updatedAt: "2026-05-30T00:00:00Z",
  };
}

async function setup() {
  const activeThreadIDRef = { current: null as string | null };
  const hook = renderHook(() =>
    useThreadData({
      abortAllStreamRuns: vi.fn(),
      activeThreadIDRef,
      handleActionError: (_error, fallback, setError) => setError(fallback),
      onSessionExpired: vi.fn(),
    }),
  );
  await waitFor(() => expect(hook.result.current.threadDataLoaded).toBe(true));
  return { hook, activeThreadIDRef };
}

beforeEach(() => {
  api.listProjects.mockResolvedValue([]);
  api.listThreads.mockResolvedValue({ items: [], nextCursor: null });
});

// A thread that fails to load must not leave the previous thread on screen
// while the route names the new one: the composer's draft scope and the send
// target follow the active thread, so a send would go to the wrong chat.
test("a failed thread load clears the previous thread instead of keeping it live", async () => {
  const { hook, activeThreadIDRef } = await setup();
  api.getThread.mockResolvedValueOnce({
    thread: thread("a"),
    messages: [{ id: "m1", threadId: "a", role: "user", content: "hi" }],
  });
  act(() => {
    hook.result.current.loadRoute({ view: "thread", threadID: "a" });
  });
  await waitFor(() => expect(hook.result.current.activeThread?.id).toBe("a"));
  expect(hook.result.current.messages).toHaveLength(1);

  api.getThread.mockRejectedValueOnce(new Error("failed to load thread"));
  act(() => {
    hook.result.current.loadRoute({ view: "thread", threadID: "b" });
  });
  await waitFor(() => expect(hook.result.current.loadError).not.toBe(""));
  expect(hook.result.current.activeThread).toBeNull();
  expect(hook.result.current.messages).toHaveLength(0);
  expect(activeThreadIDRef.current).toBeNull();
});

test("a successful load clears a previous load error", async () => {
  const { hook } = await setup();
  api.getThread.mockRejectedValueOnce(new Error("failed to load thread"));
  act(() => {
    hook.result.current.loadRoute({ view: "thread", threadID: "b" });
  });
  await waitFor(() => expect(hook.result.current.loadError).not.toBe(""));

  api.getThread.mockResolvedValueOnce({ thread: thread("c"), messages: [] });
  act(() => {
    hook.result.current.loadRoute({ view: "thread", threadID: "c" });
  });
  await waitFor(() => expect(hook.result.current.activeThread?.id).toBe("c"));
  expect(hook.result.current.loadError).toBe("");
});
