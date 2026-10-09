import { request, requestJSON } from "./http";
import type { PublicShare, ShareInfo, ShareListItem } from "./types";

// ShareNotFoundError signals a missing, disabled, or deleted share — the public
// viewer renders its "not found" state for it (never a sign-in redirect).
export class ShareNotFoundError extends Error {
  constructor() {
    super("share not found");
  }
}

const shareUrl = (threadId: string) =>
  `/api/threads/${encodeURIComponent(threadId)}/share`;

// createShare creates (or returns the existing) public share for a thread.
export async function createShare(threadId: string): Promise<ShareInfo> {
  return requestJSON(shareUrl(threadId), "failed to create share", {
    method: "POST",
  });
}

// updateShare re-freezes the snapshot of an existing share (same link).
export async function updateShare(threadId: string): Promise<ShareInfo> {
  return requestJSON(`${shareUrl(threadId)}:update`, "failed to update share", {
    method: "POST",
  });
}

// disableShare turns the public link off (the "Keep private" action).
export async function disableShare(threadId: string): Promise<void> {
  await request(
    shareUrl(threadId),
    "failed to disable share",
    { method: "DELETE" },
    [404],
  );
}

export async function getMyShares(): Promise<ShareListItem[]> {
  const page = await requestJSON<{ items: ShareListItem[] }>(
    "/api/shares",
    "failed to load shares",
  );
  return page.items ?? [];
}

// getPublicShare loads a public snapshot. It deliberately does NOT use expectJSON:
// a 401/404 here means the share is gone, not that the viewer's session expired —
// the viewer is anonymous. Both map to ShareNotFoundError.
export async function getPublicShare(shareId: string): Promise<PublicShare> {
  const response = await fetch(`/api/shares/${encodeURIComponent(shareId)}`);
  if (response.status === 404 || response.status === 401) {
    throw new ShareNotFoundError();
  }
  if (!response.ok) {
    throw new Error("failed to load shared conversation");
  }
  return response.json() as Promise<PublicShare>;
}
