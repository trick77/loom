import { AuthExpiredError, expectJSON } from "./http";
import type { Page, Thread, ThreadContentHit, ThreadResponse } from "./types";

export async function listThreads(params: {
  projectId?: string | null;
  starred?: boolean;
  archived?: boolean;
  search?: string;
  limit?: number;
  cursor?: string | null;
} = {}): Promise<Page<Thread>> {
  const query = new URLSearchParams();
  if (params.projectId !== undefined) {
    query.set("projectId", params.projectId === null ? "null" : params.projectId);
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
  if (params.cursor !== undefined && params.cursor !== null && params.cursor !== "") {
    query.set("cursor", params.cursor);
  }
  const suffix = query.toString() === "" ? "" : `?${query.toString()}`;
  const response = await fetch(`/api/threads${suffix}`);
  return expectJSON<Page<Thread>>(response, "failed to load threads");
}

// listThreadIds returns the ids of every thread matching the search, with no
// pagination — used by "select all matches" so the client can act on threads
// it has not loaded into the list.
export async function listThreadIds(params: { search?: string } = {}): Promise<string[]> {
  const query = new URLSearchParams();
  if (params.search !== undefined && params.search !== "") {
    query.set("search", params.search);
  }
  const suffix = query.toString() === "" ? "" : `?${query.toString()}`;
  const response = await fetch(`/api/threads/ids${suffix}`);
  return expectJSON<string[]>(response, "failed to load thread ids");
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
  if (params.projectId !== undefined && params.projectId !== null && params.projectId !== "") {
    query.set("projectId", params.projectId);
  }
  const response = await fetch(`/api/threads/search?${query.toString()}`);
  const body = await expectJSON<{ items: Array<Thread & { snippet: string }> }>(
    response,
    "failed to search threads",
  );
  return body.items.map(({ snippet, ...thread }) => ({ thread, snippet }));
}

export async function createThread(input: { projectId?: string | null; title?: string } = {}): Promise<Thread> {
  const response = await fetch("/api/threads", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return expectJSON<Thread>(response, "failed to create thread");
}

export async function getThread(threadId: string): Promise<ThreadResponse> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}`);
  return expectJSON<ThreadResponse>(response, "failed to load thread");
}

export async function setThreadStarred(threadId: string, starred: boolean): Promise<Thread> {
  const action = starred ? "star" : "unstar";
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/${action}`, {
    method: "POST",
  });
  return expectJSON<Thread>(response, "failed to update thread");
}

export async function updateThread(
  threadId: string,
  input: { title?: string; projectId?: string | null },
): Promise<Thread> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  return expectJSON<Thread>(response, "failed to update thread");
}

export async function deleteThread(threadId: string): Promise<void> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}`, {
    method: "DELETE",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to delete thread");
  }
}

export async function bulkDeleteThreads(threadIds: string[]): Promise<{ deleted: number }> {
  const response = await fetch("/api/threads:delete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ threadIds }),
  });
  return expectJSON<{ deleted: number }>(response, "failed to delete threads");
}

export async function stopMessage(threadId: string): Promise<void> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/messages:stop`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to stop message");
  }
}
