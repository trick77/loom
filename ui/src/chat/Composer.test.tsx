import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { Composer } from "./Composer";
import type { ComposerAttachment } from "./useDocumentAttachments";

const fileDrag = {
  dataTransfer: {
    types: ["Files"],
    files: [new File(["hello"], "notes.txt", { type: "text/plain" })],
    dropEffect: "none",
  },
};

test("shows drop guidance while files are dragged over the composer", () => {
  render(
    <Composer
      variant="chat"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
    />,
  );

  const composer = screen.getByRole("textbox").closest("form");
  expect(composer).not.toBeNull();
  expect(screen.queryByText("Drop files here to add to chat")).not.toBeInTheDocument();

  fireEvent.dragEnter(composer!, fileDrag);

  expect(screen.getByText("Drop files here to add to chat")).toBeInTheDocument();

  fireEvent.dragLeave(composer!, fileDrag);

  expect(screen.queryByText("Drop files here to add to chat")).not.toBeInTheDocument();
});

test("ignores non-file drags for composer drop guidance", () => {
  render(
    <Composer
      variant="start"
      draft=""
      isSending={false}
      placeholder="How can I help you today?"
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
    />,
  );

  const composer = screen.getByRole("textbox").closest("form");
  expect(composer).not.toBeNull();

  fireEvent.dragEnter(composer!, {
    dataTransfer: {
      types: ["text/plain"],
      files: [],
      dropEffect: "none",
    },
  });

  expect(screen.queryByText("Drop files here to add to chat")).not.toBeInTheDocument();
});

test("shows generic drop guidance on the start composer", () => {
  render(
    <Composer
      variant="start"
      draft=""
      isSending={false}
      placeholder="How can I help you today?"
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
    />,
  );

  const composer = screen.getByRole("textbox").closest("form");
  expect(composer).not.toBeNull();

  fireEvent.dragEnter(composer!, fileDrag);

  // The start composer predates any chat, so the guidance must not say "chat".
  expect(screen.getByText("Drop files here to attach")).toBeInTheDocument();
  expect(screen.queryByText("Drop files here to add to chat")).not.toBeInTheDocument();
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
