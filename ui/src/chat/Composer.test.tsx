import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";

import { Composer } from "./Composer";
import { PASTE_AS_ATTACHMENT_LINE_THRESHOLD, PASTE_AS_ATTACHMENT_THRESHOLD } from "./pastedText";
import type { ComposerAttachment } from "./useDocumentAttachments";

afterEach(() => {
  vi.restoreAllMocks();
});

test("reports unsupported picker files instead of silently ignoring them", () => {
  const onAttachFiles = vi.fn();
  const onAttachError = vi.fn();
  render(
    <Composer
      variant="thread"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
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

  const zip = new File(["binary"], "archive.zip", { type: "application/zip" });
  fireEvent.change(input!, { target: { files: [zip] } });

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
      variant="thread"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
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

test("focuses the textarea and moves the caret to the end when focusSignal changes", () => {
  function renderComposer(focusSignal: number) {
    return (
      <Composer
        variant="thread"
        draft="retry me"
        focusSignal={focusSignal}
        isSending={false}
        placeholder="Write a message..."
        reasoningEffort="high"
        onReasoningEffortChange={() => undefined}
        onDraftChange={() => undefined}
        onSend={() => undefined}
        onStop={() => undefined}
      />
    );
  }
  const { rerender } = render(renderComposer(0));
  const textarea = screen.getByRole("textbox") as HTMLTextAreaElement;
  // Mount does not steal focus (signal unchanged from its initial value).
  expect(textarea).not.toHaveFocus();

  rerender(renderComposer(1));
  expect(textarea).toHaveFocus();
  expect(textarea.selectionStart).toBe("retry me".length);
  expect(textarea.selectionEnd).toBe("retry me".length);
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
      variant="thread"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      attachments={attachments}
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
      onRemoveAttachment={() => undefined}
    />,
  );

  expect(screen.getByText("quarterly-report.pdf")).toBeInTheDocument();
  expect(screen.getByText("PDF")).toBeInTheDocument();
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
      variant="thread"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      attachments={attachments}
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAttachFiles={vi.fn()}
      onRemoveAttachment={() => undefined}
    />,
  );

  expect(document.querySelector('img[src="blob:image-preview"]')).toBeInTheDocument();
  // The image thumbnail carries its type as a pill badge overlaid inside the
  // image — identical to how it renders once sent, so the two read the same end
  // to end. The filename text card is dropped for images (it could be clipped).
  expect(screen.getByText("PNG")).toBeInTheDocument();
  expect(screen.queryByText("screenshot.png")).not.toBeInTheDocument();
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
      variant="thread"
      draft={"Long draft\n".repeat(60)}
      isSending={false}
      placeholder="Write a message..."
      attachments={attachments}
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
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
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
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

test("collapses an oversized paste into a pasted-text chip", () => {
  const onAddPastedText = vi.fn();
  const onDraftChange = vi.fn();
  render(
    <Composer
      variant="thread"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
      onDraftChange={onDraftChange}
      onSend={() => undefined}
      onStop={() => undefined}
      onAddPastedText={onAddPastedText}
    />,
  );

  const text = "A".repeat(PASTE_AS_ATTACHMENT_THRESHOLD + 1);
  const notCancelled = fireEvent.paste(screen.getByRole("textbox"), {
    clipboardData: { getData: () => text },
  });

  // preventDefault was called, so the browser's inline insertion is suppressed.
  expect(notCancelled).toBe(false);
  expect(onAddPastedText).toHaveBeenCalledWith(text);
  expect(onDraftChange).not.toHaveBeenCalled();
});

test("drops the selected draft text when a large paste replaces a selection", () => {
  const onAddPastedText = vi.fn();
  const onDraftChange = vi.fn();
  render(
    <Composer
      variant="thread"
      draft="old draft"
      isSending={false}
      placeholder="Write a message..."
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
      onDraftChange={onDraftChange}
      onSend={() => undefined}
      onStop={() => undefined}
      onAddPastedText={onAddPastedText}
    />,
  );

  const textbox = screen.getByRole("textbox") as HTMLTextAreaElement;
  // Select the entire existing draft, as a "select-all then paste to replace" gesture.
  textbox.selectionStart = 0;
  textbox.selectionEnd = "old draft".length;
  const text = "A".repeat(PASTE_AS_ATTACHMENT_THRESHOLD + 1);
  fireEvent.paste(textbox, { clipboardData: { getData: () => text } });

  // The selected text is removed so it is not sent alongside the pasted block.
  expect(onDraftChange).toHaveBeenCalledWith("");
  expect(onAddPastedText).toHaveBeenCalledWith(text);
});

test("collapses a tall paste that exceeds the line threshold under the char limit", () => {
  const onAddPastedText = vi.fn();
  render(
    <Composer
      variant="thread"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAddPastedText={onAddPastedText}
    />,
  );

  // Many short lines: well under the character limit, but past the line threshold.
  const text = "x\n".repeat(PASTE_AS_ATTACHMENT_LINE_THRESHOLD + 1);
  expect(text.length).toBeLessThanOrEqual(PASTE_AS_ATTACHMENT_THRESHOLD);
  const notCancelled = fireEvent.paste(screen.getByRole("textbox"), {
    clipboardData: { getData: () => text },
  });

  expect(notCancelled).toBe(false);
  expect(onAddPastedText).toHaveBeenCalledWith(text);
});

test("leaves a paste at the threshold inline", () => {
  const onAddPastedText = vi.fn();
  render(
    <Composer
      variant="thread"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAddPastedText={onAddPastedText}
    />,
  );

  const text = "A".repeat(PASTE_AS_ATTACHMENT_THRESHOLD);
  const notCancelled = fireEvent.paste(screen.getByRole("textbox"), {
    clipboardData: { getData: () => text },
  });

  // Default paste proceeds (not cancelled) and no chip is created.
  expect(notCancelled).toBe(true);
  expect(onAddPastedText).not.toHaveBeenCalled();
});

test("renders a pasted-text chip with a preview and badge, and removes it", () => {
  const onRemovePastedText = vi.fn();
  render(
    <Composer
      variant="thread"
      draft=""
      isSending={false}
      placeholder="Write a message..."
      reasoningEffort="high"
      onReasoningEffortChange={() => undefined}
      onDraftChange={() => undefined}
      onSend={() => undefined}
      onStop={() => undefined}
      onAddPastedText={vi.fn()}
      pastedTexts={[{ id: "pasted-1", text: "first line\nsecond line", lineCount: 2 }]}
      onRemovePastedText={onRemovePastedText}
    />,
  );

  expect(screen.getByText("Pasted")).toBeInTheDocument();
  expect(screen.getByText(/first line/)).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Remove pasted text, 2 lines" }));

  expect(onRemovePastedText).toHaveBeenCalledWith("pasted-1");
});
