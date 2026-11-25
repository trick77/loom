import { request, requestJSON } from "./http";
import type {
  Artifact,
  ArtifactListType,
  ArtifactSort,
  ArtifactSummary,
  Page,
  SortOrder,
} from "./types";

// since/until are RFC 3339 bounds on the creation time (since inclusive, until
// exclusive); empty or absent means unbounded.
export async function listArtifacts(
  params: {
    type?: ArtifactListType;
    sort?: ArtifactSort;
    order?: SortOrder;
    search?: string;
    since?: string;
    until?: string;
    limit?: number;
    cursor?: string | null;
  } = {},
): Promise<Page<Artifact>> {
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
  if (params.since !== undefined && params.since !== "") {
    query.set("since", params.since);
  }
  if (params.until !== undefined && params.until !== "") {
    query.set("until", params.until);
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
  return requestJSON(`/api/artifacts${suffix}`, "failed to load artifacts");
}

// getArtifactSummary returns how many artifacts predate since and which months
// hold artifacts at all, for the given type filter. The months are cut in the
// browser's zone (tz), the one the page lists a month in; without one the
// server cuts them in UTC.
export async function getArtifactSummary(params: {
  since: string;
  type: ArtifactListType;
}): Promise<ArtifactSummary> {
  const query = new URLSearchParams({ since: params.since, type: params.type });
  const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  if (timeZone) query.set("tz", timeZone);
  return requestJSON(
    `/api/artifacts/summary?${query.toString()}`,
    "failed to load artifact summary",
  );
}

export async function downloadArtifact(downloadUrl: string): Promise<Blob> {
  const response = await request(downloadUrl, "failed to download artifact");
  return response.blob();
}

// deleteArtifact removes an uploaded artifact (row + file) server-side. Used by
// the composer's remove path so a composer-uploaded image isn't orphaned. Only
// call it for artifacts the composer itself uploaded — never for re-attached
// existing artifacts (e.g. a generated image), which must outlive the removal.
export async function deleteArtifact(artifactId: string): Promise<void> {
  await request(
    `/api/artifacts/${encodeURIComponent(artifactId)}`,
    "failed to delete artifact",
    { method: "DELETE" },
  );
}

// renameArtifact changes an artifact's display filename. The new name propagates
// into the chat transcript where the artifact appears via the server's read-time
// overlay, so no message rewrite is needed client-side.
export async function renameArtifact(
  artifactId: string,
  displayFilename: string,
): Promise<void> {
  await request(
    `/api/artifacts/${encodeURIComponent(artifactId)}`,
    "failed to rename artifact",
    { method: "PATCH", json: { displayFilename } },
  );
}
