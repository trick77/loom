import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import type { Message } from "../api";
import { MessageBubble } from "./messages";
import type { ComposerAttachment } from "./useDocumentAttachments";

test("renders sent attachments above the user message text", () => {
  const message: Message & { attachments: ComposerAttachment[] } = {
    id: "m1",
    threadId: "t1",
    role: "user",
    content: "Summarize this document",
    createdAt: "2026-06-14T00:00:00Z",
    attachments: [
      {
        id: "att-1",
        filename: "briefing.pdf",
        mimeType: "application/pdf",
        sizeBytes: 2048,
        status: "ready",
      },
    ],
  };

  render(<MessageBubble message={message} retryContent={null} onRetry={vi.fn()} />);

  const attachment = screen.getByText("briefing.pdf");
  const text = screen.getByText("Summarize this document");

  expect(attachment).toBeInTheDocument();
  expect(text).toBeInTheDocument();
  expect(attachment.compareDocumentPosition(text) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
});

test("revokes sent attachment preview URLs when they unmount", () => {
  const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  const message: Message & { attachments: ComposerAttachment[] } = {
    id: "m1",
    threadId: "t1",
    role: "user",
    content: "",
    createdAt: "2026-06-14T00:00:00Z",
    attachments: [
      {
        id: "att-1",
        filename: "screenshot.png",
        mimeType: "image/png",
        sizeBytes: 2048,
        status: "ready",
        previewUrl: "blob:image-preview",
      },
    ],
  };

  const { unmount } = render(<MessageBubble message={message} retryContent={null} onRetry={vi.fn()} />);
  unmount();

  expect(revoke).toHaveBeenCalledWith("blob:image-preview");
});
