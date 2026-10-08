import { expectOK, request, requestJSON } from "./http";
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

// Bound on the stop round-trip. Callers await this before aborting the stream
// fetch so the attributed stop cause wins the server-side cancel race; the timeout
// caps how long a hung stop endpoint can defer that abort. On timeout the request
// rejects and the caller falls through to a plain abort (logged as request_context).
const stopMessageTimeoutMs = 4000;

// source labels which UI action triggered the stop ("stop_button", "escape") so the
// backend can attribute the cancellation in its logs. Callers should await this
// before aborting the stream fetch so the stop cause wins the server-side cancel
// race over the raw request-context drop. Resolves false when the server had no
// stream registered yet (409): only dropping the fetch stops that turn.
export async function stopMessage(
  threadId: string,
  source?: string,
): Promise<boolean> {
  const query = source ? `?source=${encodeURIComponent(source)}` : "";
  const response = await fetch(
    `/api/threads/${encodeURIComponent(threadId)}/messages:stop${query}`,
    {
      method: "POST",
      signal: AbortSignal.timeout(stopMessageTimeoutMs),
    },
  );
  await expectOK(response, "failed to stop message", [409]);
  return response.status !== 409;
}
