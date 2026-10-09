import { request, requestJSON } from "./http";
import type { Page, Thread, ThreadContentHit, ThreadResponse } from "./types";

const threadUrl = (threadId: string) =>
  `/api/threads/${encodeURIComponent(threadId)}`;

export async function listThreads(
  params: {
    projectId?: string | null;
    starred?: boolean;
    archived?: boolean;
    search?: string;
    limit?: number;
    cursor?: string | null;
  } = {},
): Promise<Page<Thread>> {
  const query = new URLSearchParams();
  if (params.projectId !== undefined) {
    query.set(
      "projectId",
      params.projectId === null ? "null" : params.projectId,
    );
  }
  if (params.starred !== undefined) {
    query.set("starred", String(params.starred));
  }
  if (params.archived !== undefined) {
    query.set("archived", String(params.archived));
  }
  if (params.search !== undefined && params.search !== "") {
    query.set("search", params.search);
  }
  if (params.limit !== undefined) {
    query.set("limit", String(params.limit));
  }
  if (
    params.cursor !== undefined &&
    params.cursor !== null &&
    params.cursor !== ""
  ) {
    query.set("cursor", params.cursor);
  }
  const suffix = query.toString() === "" ? "" : `?${query.toString()}`;
  return requestJSON(`/api/threads${suffix}`, "failed to load threads");
}

// listThreadIds returns the ids of every thread matching the search, with no
// pagination — used by "select all matches" so the client can act on threads
// it has not loaded into the list.
export async function listThreadIds(
  params: { search?: string } = {},
): Promise<string[]> {
  const query = new URLSearchParams();
  if (params.search !== undefined && params.search !== "") {
    query.set("search", params.search);
  }
  const suffix = query.toString() === "" ? "" : `?${query.toString()}`;
  return requestJSON(`/api/threads/ids${suffix}`, "failed to load thread ids");
}

// searchThreadContent runs the slower full-text search over message content
// (prefix-matched, so "vp" finds "vpn"). Returns at most `limit` threads, most
// relevant first, one per thread. Complements the fast title search
// (listThreads with `search`); the sidebar/threads search merges the two.
export async function searchThreadContent(params: {
  query: string;
  limit?: number;
  projectId?: string | null;
}): Promise<ThreadContentHit[]> {
  const query = new URLSearchParams();
  query.set("q", params.query);
  if (params.limit !== undefined) {
    query.set("limit", String(params.limit));
  }
  if (
    params.projectId !== undefined &&
    params.projectId !== null &&
    params.projectId !== ""
  ) {
    query.set("projectId", params.projectId);
  }
  const body = await requestJSON<{
    items: Array<Thread & { snippet: string }>;
  }>(`/api/threads/search?${query.toString()}`, "failed to search threads");
  return body.items.map(({ snippet, ...thread }) => ({ thread, snippet }));
}

// What the server stores when a thread's title normalizes away to nothing
// (chat.DefaultThreadTitle). It is English text in the database, so a UI that is
// not English swaps in its own translation before showing it.
export const DEFAULT_THREAD_TITLE = "New thread";

export async function createThread(
  input: { projectId?: string | null; title?: string } = {},
): Promise<Thread> {
  return requestJSON("/api/threads", "failed to create thread", {
    method: "POST",
    json: input,
  });
}

export async function getThread(threadId: string): Promise<ThreadResponse> {
  return requestJSON(threadUrl(threadId), "failed to load thread");
}

export async function setThreadStarred(
  threadId: string,
  starred: boolean,
): Promise<Thread> {
  const action = starred ? "star" : "unstar";
  return requestJSON(
    `${threadUrl(threadId)}/${action}`,
    "failed to update thread",
    { method: "POST" },
  );
}

export async function updateThread(
  threadId: string,
  input: { title?: string; projectId?: string | null },
): Promise<Thread> {
  return requestJSON(threadUrl(threadId), "failed to update thread", {
    method: "PATCH",
    json: input,
  });
}

export async function deleteThread(threadId: string): Promise<void> {
  await request(threadUrl(threadId), "failed to delete thread", {
    method: "DELETE",
  });
}

export async function bulkDeleteThreads(
  threadIds: string[],
): Promise<{ deleted: number }> {
  return requestJSON("/api/threads:delete", "failed to delete threads", {
    method: "POST",
    json: { threadIds },
  });
}

// Bound on the stop round-trip. On timeout the request rejects: the stop did
// not reach the server, and the turn keeps running there.
const stopMessageTimeoutMs = 4000;

// stopMessage asks the server to end the thread's turn. The turn runs detached
// from the client, so this is the only way to stop it: dropping the stream
// fetch does not. source labels the UI action ("stop_button", "escape") for the
// server's logs. sendId names the send whose turn to end; a stop that arrives
// before that turn registers is kept for it, and resolves false (409).
export async function stopMessage(
  threadId: string,
  source?: string,
  sendId?: string,
): Promise<boolean> {
  const params: string[] = [];
  if (source) params.push(`source=${encodeURIComponent(source)}`);
  if (sendId) params.push(`sendId=${encodeURIComponent(sendId)}`);
  const query = params.length > 0 ? `?${params.join("&")}` : "";
  const response = await request(
    `/api/threads/${encodeURIComponent(threadId)}/messages:stop${query}`,
    "failed to stop message",
    {
      method: "POST",
      signal: AbortSignal.timeout(stopMessageTimeoutMs),
    },
    [409],
  );
  return response.status !== 409;
}
