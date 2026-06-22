import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

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

afterEach(() => {
  vi.unstubAllGlobals();
});

test("renders a fenced SVG response inline as a sandboxed image with download and lightbox", () => {
  let svgBlob: Blob | undefined;
  const createObjectURL = vi.fn((blob: Blob) => {
    svgBlob = blob;
    return "blob:svg-preview";
  });
  vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL: vi.fn() });

  const message: Message = {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: '```svg\n<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>\n```',
    createdAt: "2026-06-14T00:00:00Z",
  };

  render(<MessageBubble message={message} retryContent={null} onRetry={vi.fn()} />);

  // The SVG renders via an <img> blob URL (secure-image mode) — not inline DOM —
  // and the blob is typed image/svg+xml so the browser will actually paint it.
  const preview = document.querySelector('img[src="blob:svg-preview"]');
  expect(preview).toBeInTheDocument();
  expect(svgBlob?.type).toBe("image/svg+xml");
  expect(screen.getByRole("button", { name: "Download SVG response" })).toBeInTheDocument();

  // Clicking the preview opens the shared lightbox.
  fireEvent.click(screen.getByRole("button", { name: "Preview SVG response" }));
  expect(screen.getByRole("dialog", { name: "Preview SVG response" })).toBeInTheDocument();
});

test("revokes the SVG preview object URL when the bubble unmounts", () => {
  const revokeObjectURL = vi.fn();
  vi.stubGlobal("URL", { ...URL, createObjectURL: vi.fn(() => "blob:svg-preview"), revokeObjectURL });

  const message: Message = {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: '```svg\n<svg viewBox="0 0 10 10"></svg>\n```',
    createdAt: "2026-06-14T00:00:00Z",
  };

  const { unmount } = render(<MessageBubble message={message} retryContent={null} onRetry={vi.fn()} />);
  unmount();

  expect(revokeObjectURL).toHaveBeenCalledWith("blob:svg-preview");
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
