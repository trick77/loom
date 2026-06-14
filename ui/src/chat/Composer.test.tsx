import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { Composer } from "./Composer";
import type { ComposerAttachment } from "./useDocumentAttachments";

afterEach(() => {
  vi.restoreAllMocks();
});

test("reports unsupported picker files instead of silently ignoring them", () => {
  const onAttachFiles = vi.fn();
  const onAttachError = vi.fn();
  render(
    <Composer
      variant="chat"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={onAttachFiles}
      onAttachError={onAttachError}
    />,
  );

  const composer = screen.getByRole("textbox").closest("form");
  expect(composer).not.toBeNull();
  const input = composer!.querySelector('input[type="file"]');
  expect(input).not.toBeNull();

  fireEvent.change(input!, {
    target: {
      files: [new File(["binary"], "installer.exe", { type: "application/octet-stream" })],
    },
  });

  expect(onAttachFiles).not.toHaveBeenCalled();
  expect(onAttachError).toHaveBeenCalledWith(
    "Unsupported file type. Use PDF, DOCX, PPTX, XLSX, TXT, MD, CSV, JSON, HTML, PNG, JPG, WEBP, or GIF.",
  );
});

test("attaches supported picker files and reports unsupported companions", () => {
  const onAttachFiles = vi.fn();
  const onAttachError = vi.fn();
  const note = new File(["hello"], "notes.txt", { type: "text/plain" });
  const unsupported = new File(["binary"], "installer.exe", { type: "application/octet-stream" });
  render(
    <Composer
      variant="chat"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={onAttachFiles}
      onAttachError={onAttachError}
    />,
  );

  const composer = screen.getByRole("textbox").closest("form");
  expect(composer).not.toBeNull();
  const input = composer!.querySelector('input[type="file"]');
  expect(input).not.toBeNull();

  fireEvent.change(input!, {
    target: {
      files: [note, unsupported],
    },
  });

  expect(onAttachFiles).toHaveBeenCalledWith([note]);
  expect(onAttachError).toHaveBeenCalledWith(
    "Unsupported file type. Use PDF, DOCX, PPTX, XLSX, TXT, MD, CSV, JSON, HTML, PNG, JPG, WEBP, or GIF.",
  );
});

test("renders uploading attachment previews inside the composer", () => {
  const attachments: ComposerAttachment[] = [
    {
      id: "att-1",
      filename: "quarterly-report.pdf",
      mimeType: "application/pdf",
      sizeBytes: 1024 * 120,
      status: "uploading",
    },
  ];

  render(
    <Composer
      variant="chat"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      attachments={attachments}
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
      onRemoveAttachment={() => undefined}
    />,
  );

  expect(screen.getByText("quarterly-report.pdf")).toBeInTheDocument();
  expect(screen.getByText("Uploading...")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Remove quarterly-report.pdf" })).toBeInTheDocument();
});

test("shows a thumbnail for previewable image attachments", () => {
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:image-preview");
  const attachments: ComposerAttachment[] = [
    {
      id: "att-1",
      filename: "screenshot.png",
      mimeType: "image/png",
      sizeBytes: 1024,
      status: "queued",
      previewUrl: URL.createObjectURL(new File(["png"], "screenshot.png", { type: "image/png" })),
    },
  ];

  render(
    <Composer
      variant="chat"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      attachments={attachments}
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
      onRemoveAttachment={() => undefined}
    />,
  );

  expect(document.querySelector('img[src="blob:image-preview"]')).toBeInTheDocument();
});

test("keeps attachment previews above the draft text area", () => {
  const attachments: ComposerAttachment[] = [
    {
      id: "att-1",
      filename: "diagram.pdf",
      mimeType: "application/pdf",
      sizeBytes: 1024,
      status: "ready",
    },
  ];

  render(
    <Composer
      variant="chat"
      draft={"Long draft\n".repeat(60)}
      isSending={false}
      placeholder="Write a message..."
      attachments={attachments}
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
      onRemoveAttachment={() => undefined}
    />,
  );

  const attachment = screen.getByText("diagram.pdf");
  const textbox = screen.getByRole("textbox");
  const attachmentStrip = screen.getByLabelText("Message attachments");

  expect(
    attachment.compareDocumentPosition(textbox) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).not.toBe(0);
  expect(attachmentStrip).toHaveClass("flex-none", "overflow-y-auto");
});

test("removes an attachment preview before send", () => {
  const onRemoveAttachment = vi.fn();
  const attachments: ComposerAttachment[] = [
    {
      id: "att-1",
      filename: "notes.txt",
      mimeType: "text/plain",
      sizeBytes: 18,
      status: "ready",
    },
  ];

  render(
    <Composer
      variant="start"
      draft=""
      isSending={false}
      placeholder="How can I help you today?"
      attachments={attachments}
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
      onRemoveAttachment={onRemoveAttachment}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Remove notes.txt" }));

  expect(onRemoveAttachment).toHaveBeenCalledWith("att-1");
});
