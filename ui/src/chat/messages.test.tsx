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

  render(
    <MessageBubble message={message} retryMessage={null} onRetry={vi.fn()} />,
  );

  const attachment = screen.getByText("briefing.pdf");
  const text = screen.getByText("Summarize this document");

  expect(attachment).toBeInTheDocument();
  expect(text).toBeInTheDocument();
  expect(
    attachment.compareDocumentPosition(text) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
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

  render(
    <MessageBubble message={message} retryMessage={null} onRetry={vi.fn()} />,
  );

  const images = document.querySelectorAll('img[src^="blob:"]');
  const text = screen.getByText("Explain these images");

  expect(images).toHaveLength(2);
  expect(
    images[0].closest("[data-testid='sent-image-attachment']"),
  ).toHaveClass("h-[76px]", "w-[76px]");
  expect(screen.queryByText("logo.png")).not.toBeInTheDocument();
  expect(screen.queryByText("badge.webp")).not.toBeInTheDocument();
  expect(
    images[0].compareDocumentPosition(text) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
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
    content:
      '```svg\n<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4"/></svg>\n```',
    createdAt: "2026-06-14T00:00:00Z",
  };

  render(
    <MessageBubble message={message} retryMessage={null} onRetry={vi.fn()} />,
  );

  // The SVG renders via an <img> blob URL (secure-image mode) — not inline DOM —
  // and the blob is typed image/svg+xml so the browser will actually paint it.
  const preview = document.querySelector('img[src="blob:svg-preview"]');
  expect(preview).toBeInTheDocument();
  expect(svgBlob?.type).toBe("image/svg+xml");
  expect(
    screen.getByRole("button", { name: "Download SVG response" }),
  ).toBeInTheDocument();

  // Clicking the preview opens the shared lightbox.
  fireEvent.click(screen.getByRole("button", { name: "Preview SVG response" }));
  expect(
    screen.getByRole("dialog", { name: "Preview SVG response" }),
  ).toBeInTheDocument();
});

test("revokes the SVG preview object URL when the bubble unmounts", () => {
  const revokeObjectURL = vi.fn();
  vi.stubGlobal("URL", {
    ...URL,
    createObjectURL: vi.fn(() => "blob:svg-preview"),
    revokeObjectURL,
  });

  const message: Message = {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: '```svg\n<svg viewBox="0 0 10 10"></svg>\n```',
    createdAt: "2026-06-14T00:00:00Z",
  };

  const { unmount } = render(
    <MessageBubble message={message} retryMessage={null} onRetry={vi.fn()} />,
  );
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

  const { unmount } = render(
    <MessageBubble message={message} retryMessage={null} onRetry={vi.fn()} />,
  );
  unmount();

  expect(revoke).toHaveBeenCalledWith("blob:image-preview");
});

test("renders the prompt-classifier category as a humanized pill on assistant messages", () => {
  const message: Message = {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: "Newton was a physicist.",
    createdAt: "2026-06-14T00:00:00Z",
  };

  render(
    <MessageBubble
      message={message}
      retryMessage={null}
      onRetry={vi.fn()}
      category="knowledge_discovery"
    />,
  );

  expect(screen.getByText("Knowledge Discovery")).toBeInTheDocument();
});

test("renders the url_lookup category with the URL acronym upper-cased", () => {
  const message: Message = {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: "The page is about physics.",
    createdAt: "2026-06-14T00:00:00Z",
  };

  render(
    <MessageBubble
      message={message}
      retryMessage={null}
      onRetry={vi.fn()}
      category="url_lookup"
    />,
  );

  expect(screen.getByText("URL Lookup")).toBeInTheDocument();
});

test("renders no category pill when the thread is unclassified", () => {
  const message: Message = {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: "Newton was a physicist.",
    createdAt: "2026-06-14T00:00:00Z",
  };

  render(
    <MessageBubble
      message={message}
      retryMessage={null}
      onRetry={vi.fn()}
      category=""
    />,
  );

  expect(screen.queryByText("Knowledge Discovery")).not.toBeInTheDocument();
});

test("renders the sources row above the metrics/status footer", () => {
  const message: Message = {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: "Answer with a source.",
    createdAt: "2026-06-14T00:00:00Z",
    citations: [
      {
        documentId: "",
        filename: "example.com",
        snippet: "",
        score: 0,
        url: "https://example.com/x",
        index: 1,
      },
    ],
  };

  render(
    <MessageBubble
      message={message}
      retryMessage={null}
      onRetry={vi.fn()}
      category="knowledge_discovery"
    />,
  );

  const sources = screen.getByText("Sources");
  const status = screen.getByText("Knowledge Discovery");
  // The status line must follow the sources row in document order (Sources above).
  expect(
    sources.compareDocumentPosition(status) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});

// Regression: MessageBubble derived the Sources row from the citation *numbering*,
// which drops anything without an index. Document citations only started carrying
// one recently, so a pure-RAG answer persisted before that lost its whole Sources
// row — chips, excerpt count and snippet reveal all gone.
test("shows document sources for a message persisted before documents were numbered", () => {
  const message: Message = {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: "Revenue rose sharply last quarter.",
    createdAt: "2026-06-14T00:00:00Z",
    citations: [
      { documentId: "d1", filename: "report.pdf", snippet: "low", score: 0.4 },
      {
        documentId: "d1",
        filename: "report.pdf",
        snippet: "high",
        score: 0.95,
      },
    ],
  };

  render(
    <MessageBubble message={message} retryMessage={null} onRetry={vi.fn()} />,
  );

  expect(screen.getByText("report.pdf")).toBeInTheDocument();
  expect(screen.getByText("2 excerpts")).toBeInTheDocument();
});

// The three tests below cover the wiring between an inline citation marker and the
// Sources drawer, which lives in MessageBubble and is invisible to the unit tests of
// either half: sourcePills only reports the click, SourcesSidebar only renders what
// it is handed.
function assistantWithCitations(): Message {
  const url = (host: string) => `https://${host}.example`;
  return {
    id: "a1",
    threadId: "t1",
    role: "assistant",
    content: "Alpha claims this [1]. Beta and gamma back that [2][3].",
    createdAt: "2026-06-14T00:00:00Z",
    citations: [1, 2, 3].map((index) => ({
      documentId: "",
      filename: ["Alpha", "Beta", "Gamma"][index - 1],
      snippet: `snippet ${index}`,
      score: 0.9,
      url: url(["alpha", "beta", "gamma"][index - 1]),
      index,
      title: `Title ${index}`,
    })),
  };
}

// The card of a source, found by the number it shows — the same number as the marker.
function selectedNumbers(): string[] {
  return [...document.querySelectorAll(".ui-source-card-selected")].map(
    (card) => card.querySelector(".ui-source-number")?.textContent ?? "",
  );
}

test("clicking a citation marker opens the drawer with that source pinned", () => {
  render(
    <MessageBubble
      message={assistantWithCitations()}
      retryMessage={null}
      onRetry={vi.fn()}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Alpha" }));

  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(selectedNumbers()).toEqual(["1"]);
});

// Two clicks are two questions, not one growing one — so the second replaces the
// first rather than adding to it. Nothing about the mouse clears a selection; only
// another click or closing the drawer does.
test("a second marker click replaces the pinned selection", () => {
  render(
    <MessageBubble
      message={assistantWithCitations()}
      retryMessage={null}
      onRetry={vi.fn()}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Alpha" }));
  expect(selectedNumbers()).toEqual(["1"]);

  // Beta and Gamma stand together, so they are one citation: clicking either pins
  // both, and Alpha is dropped.
  fireEvent.click(screen.getByRole("button", { name: "Beta" }));
  expect(selectedNumbers()).toEqual(["2", "3"]);
});

test("closing the drawer drops the selection", () => {
  render(
    <MessageBubble
      message={assistantWithCitations()}
      retryMessage={null}
      onRetry={vi.fn()}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Alpha" }));
  fireEvent.click(screen.getByRole("button", { name: "Close" }));
  fireEvent.click(screen.getByRole("button", { name: /sources/i }));

  expect(screen.getByRole("dialog")).toBeInTheDocument();
  expect(selectedNumbers()).toEqual([]);
});
