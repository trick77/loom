import { UserFacingError, expectJSON, expectOK } from "./http";
import {
  DOCUMENT_MAX_THREAD_ATTACHMENTS,
  type Artifact,
  type Document,
} from "./types";
import i18n from "../i18n";

export async function uploadDocument(
  file: File,
  opts: { threadId?: string; projectId?: string; signal?: AbortSignal } = {},
): Promise<Document> {
  const form = new FormData();
  form.append("file", file);
  if (opts.threadId) form.append("threadId", opts.threadId);
  if (opts.projectId) form.append("projectId", opts.projectId);
  const response = await fetch("/api/documents/upload", {
    method: "POST",
    body: form,
    signal: opts.signal,
  });
  if (response.status === 415) {
    throw new UserFacingError(i18n.t("errors.unsupportedDocumentFormat"));
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
  return expectJSON<Document>(response, "failed to upload document");
}

export async function uploadImageAttachment(
  file: File,
  opts: { threadId?: string; projectId?: string; signal?: AbortSignal } = {},
): Promise<Artifact> {
  const form = new FormData();
  form.append("file", file);
  if (opts.threadId) form.append("threadId", opts.threadId);
  if (opts.projectId) form.append("projectId", opts.projectId);
  const response = await fetch("/api/artifacts/images/upload", {
    method: "POST",
    body: form,
    signal: opts.signal,
  });
  if (response.status === 415) {
    throw new UserFacingError(i18n.t("errors.unsupportedImageFormat"));
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
  return expectJSON<Artifact>(response, "failed to upload image");
}

export async function listDocuments(projectId?: string): Promise<Document[]> {
  const suffix = projectId ? `?projectId=${encodeURIComponent(projectId)}` : "";
  const response = await fetch(`/api/documents${suffix}`);
  const body = await expectJSON<{ items: Document[] }>(
    response,
    "failed to load documents",
  );
  return body.items ?? [];
}

export async function indexDocument(documentId: string): Promise<Document> {
  const response = await fetch(
    `/api/documents/${encodeURIComponent(documentId)}/index`,
    {
      method: "POST",
    },
  );
  return expectJSON<Document>(response, "failed to index document");
}

export async function unindexDocument(documentId: string): Promise<void> {
  const response = await fetch(
    `/api/documents/${encodeURIComponent(documentId)}/unindex`,
    {
      method: "POST",
    },
  );
  await expectOK(response, "failed to unindex document");
}

export async function deleteDocument(documentId: string): Promise<void> {
  const response = await fetch(
    `/api/documents/${encodeURIComponent(documentId)}`,
    {
      method: "DELETE",
    },
  );
  await expectOK(response, "failed to delete document");
}
