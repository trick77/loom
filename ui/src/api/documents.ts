import { AuthExpiredError, expectJSON } from "./http";
import { DOCUMENT_MAX_THREAD_ATTACHMENTS, type Artifact, type Document } from "./types";

export async function uploadDocument(
  file: File,
  opts: { threadId?: string; projectId?: string } = {},
): Promise<Document> {
  const form = new FormData();
  form.append("file", file);
  if (opts.threadId) form.append("threadId", opts.threadId);
  if (opts.projectId) form.append("projectId", opts.projectId);
  const response = await fetch("/api/documents/upload", { method: "POST", body: form });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (response.status === 415) {
    throw new Error("Unsupported document format");
  }
  if (response.status === 409) {
    throw new Error(`A thread can have up to ${DOCUMENT_MAX_THREAD_ATTACHMENTS} attached files.`);
  }
  if (response.status === 413) {
    throw new Error("Files must be 25 MB or smaller.");
  }
  return expectJSON<Document>(response, "failed to upload document");
}

export async function uploadImageAttachment(
  file: File,
  opts: { threadId?: string; projectId?: string } = {},
): Promise<Artifact> {
  const form = new FormData();
  form.append("file", file);
  if (opts.threadId) form.append("threadId", opts.threadId);
  if (opts.projectId) form.append("projectId", opts.projectId);
  const response = await fetch("/api/artifacts/images/upload", { method: "POST", body: form });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (response.status === 415) {
    throw new Error("Unsupported image format");
  }
  if (response.status === 409) {
    throw new Error(`A thread can have up to ${DOCUMENT_MAX_THREAD_ATTACHMENTS} attached files.`);
  }
  if (response.status === 413) {
    throw new Error("Files must be 25 MB or smaller.");
  }
  return expectJSON<Artifact>(response, "failed to upload image");
}

export async function listDocuments(projectId?: string): Promise<Document[]> {
  const suffix = projectId ? `?projectId=${encodeURIComponent(projectId)}` : "";
  const response = await fetch(`/api/documents${suffix}`);
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  const body = await expectJSON<{ items: Document[] }>(response, "failed to load documents");
  return body.items ?? [];
}

export async function indexDocument(documentId: string): Promise<Document> {
  const response = await fetch(`/api/documents/${encodeURIComponent(documentId)}/index`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  return expectJSON<Document>(response, "failed to index document");
}

export async function unindexDocument(documentId: string): Promise<void> {
  const response = await fetch(`/api/documents/${encodeURIComponent(documentId)}/unindex`, {
    method: "POST",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to unindex document");
  }
}

export async function deleteDocument(documentId: string): Promise<void> {
  const response = await fetch(`/api/documents/${encodeURIComponent(documentId)}`, {
    method: "DELETE",
  });
  if (response.status === 401) {
    throw new AuthExpiredError();
  }
  if (!response.ok) {
    throw new Error("failed to delete document");
  }
}
