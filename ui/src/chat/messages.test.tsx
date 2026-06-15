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

test("renders sent images as compact thumbnails without file text", () => {
  const message: Message & { attachments: ComposerAttachment[] } = {
    id: "m1",
    threadId: "t1",
    role: "user",
    content: "Explain these images",
    createdAt: "2026-06-14T00:00:00Z",
    attachments: [
      {
        id: "att-1",
        filename: "logo.png",
        mimeType: "image/png",
        sizeBytes: 2048,
        status: "ready",
        previewUrl: "blob:logo",
      },
      {
        id: "att-2",
        filename: "badge.webp",
        mimeType: "image/webp",
        sizeBytes: 4096,
        status: "ready",
        previewUrl: "blob:badge",
      },
    ],
  };

  render(<MessageBubble message={message} retryContent={null} onRetry={vi.fn()} />);

  const images = document.querySelectorAll('img[src^="blob:"]');
  const text = screen.getByText("Explain these images");

  expect(images).toHaveLength(2);
  expect(images[0].closest("[data-testid='sent-image-attachment']")).toHaveClass("h-[76px]", "w-[76px]");
  expect(screen.queryByText("logo.png")).not.toBeInTheDocument();
  expect(screen.queryByText("badge.webp")).not.toBeInTheDocument();
  expect(images[0].compareDocumentPosition(text) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
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
