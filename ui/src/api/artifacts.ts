import { AuthExpiredError, expectJSON } from "./http";
import type { Artifact, ArtifactListType, ArtifactSort, Page, SortOrder } from "./types";

export async function listArtifacts(params: {
  type?: ArtifactListType;
  sort?: ArtifactSort;
  order?: SortOrder;
  search?: string;
  limit?: number;
  cursor?: string | null;
} = {}): Promise<Page<Artifact>> {
  const query = new URLSearchParams();
  if (params.type !== undefined) {
    query.set("type", params.type);
  }
  if (params.sort !== undefined) {
    query.set("sort", params.sort);
  }
  if (params.order !== undefined) {
    query.set("order", params.order);
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
  const response = await fetch(`/api/artifacts${suffix}`);
  return expectJSON<Page<Artifact>>(response, "failed to load artifacts");
}

export async function downloadArtifact(downloadUrl: string): Promise<Blob> {
  const response = await fetch(downloadUrl);
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to download artifact");
  }
  return response.blob();
}

// deleteArtifact removes an uploaded artifact (row + file) server-side. Used by
// the composer's remove path so a composer-uploaded image isn't orphaned. Only
// call it for artifacts the composer itself uploaded — never for re-attached
// existing artifacts (e.g. a generated image), which must outlive the removal.
export async function deleteArtifact(artifactId: string): Promise<void> {
  const response = await fetch(`/api/artifacts/${encodeURIComponent(artifactId)}`, {
    method: "DELETE",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to delete artifact");
  }
}

// renameArtifact changes an artifact's display filename. The new name propagates
// into the chat transcript where the artifact appears via the server's read-time
// overlay, so no message rewrite is needed client-side.
export async function renameArtifact(artifactId: string, displayFilename: string): Promise<void> {
  const response = await fetch(`/api/artifacts/${encodeURIComponent(artifactId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ displayFilename }),
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to rename artifact");
  }
}
