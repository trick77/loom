import { AuthExpiredError, expectJSON } from "./http";
import type { PublicShare, ShareInfo, ShareListItem } from "./types";

// ShareNotFoundError signals a missing, disabled, or deleted share — the public
// viewer renders its "not found" state for it (never a sign-in redirect).
export class ShareNotFoundError extends Error {
  constructor() {
    super("share not found");
  }
}

// createShare creates (or returns the existing) public share for a thread.
export async function createShare(threadId: string): Promise<ShareInfo> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/share`, {
    method: "POST",
  });
  return expectJSON<ShareInfo>(response, "failed to create share");
}

// updateShare re-freezes the snapshot of an existing share (same link).
export async function updateShare(threadId: string): Promise<ShareInfo> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/share:update`, {
    method: "POST",
  });
  return expectJSON<ShareInfo>(response, "failed to update share");
}

// disableShare turns the public link off (the "Keep private" action).
export async function disableShare(threadId: string): Promise<void> {
  const response = await fetch(`/api/threads/${encodeURIComponent(threadId)}/share`, {
    method: "DELETE",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok && response.status !== 404) {
    throw new Error("failed to disable share");
  }
}

export async function getMyShares(): Promise<ShareListItem[]> {
  const response = await fetch("/api/shares");
  const page = await expectJSON<{ items: ShareListItem[] }>(response, "failed to load shares");
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
