import { act, renderHook } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import {
  DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE,
  DOCUMENT_MAX_UPLOAD_BYTES,
  indexDocument,
  listDocuments,
  uploadDocument,
} from "../api";
import { useDocumentAttachments } from "./useDocumentAttachments";

vi.mock("../api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api")>();
  return {
    ...actual,
    indexDocument: vi.fn(),
    listDocuments: vi.fn(),
    uploadDocument: vi.fn(),
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
