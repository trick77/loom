import { act, renderHook } from "@testing-library/react";
import { expect, test } from "vitest";

import { DOCUMENT_MAX_ATTACHMENTS_PER_MESSAGE, DOCUMENT_MAX_UPLOAD_BYTES } from "../api";
import { useDocumentAttachments } from "./useDocumentAttachments";

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
