import { act, renderHook } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import {
  deleteArtifact,
  deleteDocument,
  DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
  DOCUMENT_MAX_UPLOAD_BYTES,
  indexDocument,
  listDocuments,
  uploadDocument,
  uploadImageAttachment,
} from "../api";
import {
  composerAttachmentFromArtifact,
  composerAttachmentFromMessageAttachment,
  createComposerAttachment,
  deleteUploadedAttachment,
  isImageAttachment,
  type ComposerAttachment,
  useDocumentAttachments,
} from "./useDocumentAttachments";

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api")>();
  return {
    ...actual,
    deleteArtifact: vi.fn(),
    deleteDocument: vi.fn(),
    indexDocument: vi.fn(),
    listDocuments: vi.fn(),
    uploadDocument: vi.fn(),
    uploadImageAttachment: vi.fn(),
  };
});

function file(name: string): File {
  return new File(["hello"], name, { type: "text/plain" });
}

test("limits pending composer attachments to the per-message maximum", () => {
  const { result } = renderHook(() => useDocumentAttachments({}));

  act(() => {
    result.current.handleAttachFiles(
      Array.from({ length: DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE - 1 }, (_, index) => file(`seed-${index}.txt`)),
    );
  });
  act(() => {
    result.current.handleAttachFiles([file("extra-1.txt"), file("extra-2.txt"), file("extra-3.txt")]);
  });

  expect(result.current.attachments).toHaveLength(DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE);
  expect(result.current.attachments[result.current.attachments.length - 1]?.filename).toBe("extra-1.txt");
  expect(result.current.attachNote).toBe(`You can attach up to ${DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE} files per message.`);
});

test("rejects oversized pending composer attachments", () => {
  const { result } = renderHook(() => useDocumentAttachments({}));
  const oversized = new File(["x"], "large.txt", { type: "text/plain" });
  Object.defineProperty(oversized, "size", { value: DOCUMENT_MAX_UPLOAD_BYTES + 1 });

  act(() => {
    result.current.handleAttachFiles([oversized]);
  });

  expect(result.current.attachments).toHaveLength(0);
  expect(result.current.attachNote).toBe("Files must be 25 MB or smaller.");
});

test("does not treat non-numeric drag file size as oversized", () => {
  const { result } = renderHook(() => useDocumentAttachments({}));
  const tiny = new File(["png"], "tiny.png", { type: "image/png" });
  Object.defineProperty(tiny, "size", { value: undefined });

  act(() => {
    result.current.handleAttachFiles([tiny]);
  });

  expect(result.current.attachments).toHaveLength(1);
  expect(result.current.attachNote).toBe("");
});

test("clears the composer note after a document is added to knowledge", async () => {
  vi.useFakeTimers();
  vi.mocked(uploadDocument).mockResolvedValue({
    id: "doc_1",
    filename: "notes.md",
    mimeType: "text/markdown",
    sizeBytes: 5,
    status: "pending",
    createdAt: "2026-06-14T00:00:00Z",
  });
  vi.mocked(indexDocument).mockResolvedValue({
    id: "doc_1",
    filename: "notes.md",
    mimeType: "text/markdown",
    sizeBytes: 5,
    status: "embedded",
    createdAt: "2026-06-14T00:00:00Z",
  });
  vi.mocked(listDocuments).mockResolvedValue([
    {
      id: "doc_1",
      filename: "notes.md",
      mimeType: "text/markdown",
      sizeBytes: 5,
      status: "embedded",
      createdAt: "2026-06-14T00:00:00Z",
    },
  ]);
  const { result } = renderHook(() => useDocumentAttachments({ threadId: "t1" }));

  await act(async () => {
    result.current.handleAttachFiles([file("notes.md")]);
  });

  await act(async () => {
    await vi.advanceTimersByTimeAsync(1500);
  });
  expect(result.current.attachments[0]?.status).toBe("ready");
  expect(result.current.attachNote).toBe("");
});

test("composerAttachmentFromArtifact yields a ready, re-sendable image attachment", () => {
  const attachment = composerAttachmentFromArtifact({
    id: "art-123",
    displayFilename: "cat.png",
    mimeType: "image/png",
    sizeBytes: 4096,
    downloadUrl: "/api/artifacts/art-123/download",
  });

  // Ready immediately (already persisted server-side), carries the artifact id so
  // ThreadShell wires it through as an imageAttachmentId, and uses the download URL
  // as its preview thumbnail. No File: it must skip the upload step.
  expect(attachment.status).toBe("ready");
  expect(attachment.artifactId).toBe("art-123");
  expect(attachment.previewUrl).toBe("/api/artifacts/art-123/download");
  expect(attachment.file).toBeUndefined();
  expect(isImageAttachment(attachment)).toBe(true);
});

test("composerAttachmentFromMessageAttachment rehydrates a persisted image attachment", () => {
  const attachment = composerAttachmentFromMessageAttachment({
    kind: "image",
    artifactId: "art-9",
    filename: "photo.png",
    mimeType: "image/png",
    sizeBytes: 1234,
    downloadUrl: "/api/artifacts/art-9/download",
  });

  // Ready, no File, stable id from the artifact id, and the server download URL
  // doubles as the thumbnail source so a reloaded image looks like a sent one.
  expect(attachment.id).toBe("sent-art-9");
  expect(attachment.status).toBe("ready");
  expect(attachment.artifactId).toBe("art-9");
  expect(attachment.previewUrl).toBe("/api/artifacts/art-9/download");
  expect(attachment.file).toBeUndefined();
  expect(isImageAttachment(attachment)).toBe(true);
});

test("uploadExistingAttachments awaits the document upload so its id is set before resolving", async () => {
  // Regression: a document attached on a new chat was uploaded fire-and-forget,
  // so the caller collected documentAttachmentIds before its id existed — the
  // model never saw the doc and it wasn't persisted. The flush must await the
  // upload so documentId is patched in before it resolves.
  vi.mocked(uploadDocument).mockResolvedValue({
    id: "doc_x",
    filename: "brief.txt",
    mimeType: "text/plain",
    sizeBytes: 5,
    status: "pending",
    createdAt: "2026-06-14T00:00:00Z",
  });
  vi.mocked(indexDocument).mockResolvedValue({
    id: "doc_x",
    filename: "brief.txt",
    mimeType: "text/plain",
    sizeBytes: 5,
    status: "embedded",
    createdAt: "2026-06-14T00:00:00Z",
  });
  const { result } = renderHook(() => useDocumentAttachments({}));
  const attachment = createComposerAttachment(file("brief.txt"), "queued");
  const patches: Record<string, Partial<ComposerAttachment>> = {};
  const onStatus = (id: string, patch: Partial<ComposerAttachment>) => {
    patches[id] = { ...patches[id], ...patch };
  };

  await act(async () => {
    await result.current.uploadExistingAttachments([attachment], { threadId: "t1" }, onStatus);
  });

  expect(patches[attachment.id]?.documentId).toBe("doc_x");
  expect(patches[attachment.id]?.status).toBe("ready");
});

test("composerAttachmentFromMessageAttachment rehydrates a persisted document attachment", () => {
  const attachment = composerAttachmentFromMessageAttachment({
    kind: "document",
    documentId: "doc-3",
    filename: "report.pdf",
    mimeType: "application/pdf",
    sizeBytes: 9001,
  });

  // Documents have no download endpoint yet, so no preview URL; carries the
  // document id and renders as a file pill (not an image).
  expect(attachment.id).toBe("sent-doc-3");
  expect(attachment.documentId).toBe("doc-3");
  expect(attachment.artifactId).toBeUndefined();
  expect(attachment.previewUrl).toBeUndefined();
  expect(isImageAttachment(attachment)).toBe(false);
});

test("createComposerAttachment marks the attachment as composer-uploaded", () => {
  expect(createComposerAttachment(file("n.txt")).uploadedByComposer).toBe(true);
});

test("re-attached/rehydrated attachments are NOT marked composer-uploaded (data-loss guard)", () => {
  const reAttached = composerAttachmentFromArtifact({
    id: "art-1",
    displayFilename: "gen.png",
    mimeType: "image/png",
    sizeBytes: 1,
    downloadUrl: "/api/artifacts/art-1/download",
  });
  const rehydrated = composerAttachmentFromMessageAttachment({
    kind: "image",
    artifactId: "art-2",
    filename: "p.png",
    mimeType: "image/png",
    sizeBytes: 1,
    downloadUrl: "/api/artifacts/art-2/download",
  });
  expect(reAttached.uploadedByComposer).toBeUndefined();
  expect(rehydrated.uploadedByComposer).toBeUndefined();
});

test("deleteUploadedAttachment deletes a composer-uploaded image artifact", () => {
  vi.mocked(deleteArtifact).mockClear().mockResolvedValue(undefined);
  vi.mocked(deleteDocument).mockClear();
  deleteUploadedAttachment({
    id: "a",
    filename: "p.png",
    mimeType: "image/png",
    sizeBytes: 1,
    status: "ready",
    artifactId: "art-9",
    uploadedByComposer: true,
  });
  expect(deleteArtifact).toHaveBeenCalledWith("art-9");
  expect(deleteDocument).not.toHaveBeenCalled();
});

test("deleteUploadedAttachment deletes a composer-uploaded document", () => {
  vi.mocked(deleteDocument).mockClear().mockResolvedValue(undefined);
  vi.mocked(deleteArtifact).mockClear();
  deleteUploadedAttachment({
    id: "a",
    filename: "d.pdf",
    mimeType: "application/pdf",
    sizeBytes: 1,
    status: "ready",
    documentId: "doc-3",
    uploadedByComposer: true,
  });
  expect(deleteDocument).toHaveBeenCalledWith("doc-3");
  expect(deleteArtifact).not.toHaveBeenCalled();
});

test("deleteUploadedAttachment leaves a re-attached artifact untouched (data-loss guard)", () => {
  vi.mocked(deleteArtifact).mockClear();
  vi.mocked(deleteDocument).mockClear();
  // A re-attached generated image: carries an artifactId but no uploadedByComposer.
  deleteUploadedAttachment({
    id: "a",
    filename: "gen.png",
    mimeType: "image/png",
    sizeBytes: 1,
    status: "ready",
    artifactId: "art-keep",
  });
  expect(deleteArtifact).not.toHaveBeenCalled();
  expect(deleteDocument).not.toHaveBeenCalled();
});

test("removeAttachment deletes a composer-uploaded image server-side", async () => {
  vi.mocked(deleteArtifact).mockClear().mockResolvedValue(undefined);
  vi.mocked(uploadImageAttachment).mockResolvedValue({
    id: "art_up",
    displayFilename: "p.png",
    mimeType: "image/png",
    sizeBytes: 3,
    downloadUrl: "/api/artifacts/art_up/download",
  });
  const { result } = renderHook(() => useDocumentAttachments({ threadId: "t1" }));

  await act(async () => {
    result.current.handleAttachFiles([new File(["png"], "p.png", { type: "image/png" })]);
  });
  const uploaded = result.current.attachments[0];
  expect(uploaded?.artifactId).toBe("art_up");
  expect(uploaded?.uploadedByComposer).toBe(true);

  act(() => {
    result.current.removeAttachment(uploaded.id);
  });

  expect(result.current.attachments).toHaveLength(0);
  expect(deleteArtifact).toHaveBeenCalledWith("art_up");
});
