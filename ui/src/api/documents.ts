import { UserFacingError, expectJSON, request, requestJSON } from "./http";
import {
  DOCUMENT_MAX_THREAD_ATTACHMENTS,
  type Artifact,
  type Document,
} from "./types";
import i18n from "../i18n";

type UploadOptions = {
  threadId?: string;
  projectId?: string;
  signal?: AbortSignal;
};

export function uploadDocument(
  file: File,
  opts: UploadOptions = {},
): Promise<Document> {
  return upload<Document>(
    "/api/documents/upload",
    file,
    opts,
    "errors.unsupportedDocumentFormat",
    "failed to upload document",
  );
}

export function uploadImageAttachment(
  file: File,
  opts: UploadOptions = {},
): Promise<Artifact> {
  return upload<Artifact>(
    "/api/artifacts/images/upload",
    file,
    opts,
    "errors.unsupportedImageFormat",
    "failed to upload image",
  );
}

// upload posts a file attached to a thread or project; the rejections the
// user can act on (format, attachment count, size) come back localized.
async function upload<T>(
  url: string,
  file: File,
  opts: UploadOptions,
  unsupportedKey:
    "errors.unsupportedDocumentFormat" | "errors.unsupportedImageFormat",
  errorMessage: string,
): Promise<T> {
  const form = new FormData();
  form.append("file", file);
  if (opts.threadId) form.append("threadId", opts.threadId);
  if (opts.projectId) form.append("projectId", opts.projectId);
  const response = await fetch(url, {
    method: "POST",
    body: form,
    signal: opts.signal,
  });
  if (response.status === 415) {
    throw new UserFacingError(i18n.t(unsupportedKey));
  }
  if (response.status === 409) {
    throw new UserFacingError(
      i18n.t("errors.tooManyAttachments", {
        count: DOCUMENT_MAX_THREAD_ATTACHMENTS,
      }),
    );
  }
  if (response.status === 413) {
    throw new UserFacingError(i18n.t("errors.fileTooLarge"));
  }
  return expectJSON<T>(response, errorMessage);
}

export async function listDocuments(projectId?: string): Promise<Document[]> {
  const suffix = projectId ? `?projectId=${encodeURIComponent(projectId)}` : "";
  const body = await requestJSON<{ items: Document[] }>(
    `/api/documents${suffix}`,
    "failed to load documents",
  );
  return body.items ?? [];
}

export async function indexDocument(documentId: string): Promise<Document> {
  return requestJSON(
    `/api/documents/${encodeURIComponent(documentId)}/index`,
    "failed to index document",
    { method: "POST" },
  );
}

export async function deleteDocument(documentId: string): Promise<void> {
  await request(
    `/api/documents/${encodeURIComponent(documentId)}`,
    "failed to delete document",
    { method: "DELETE" },
  );
}
