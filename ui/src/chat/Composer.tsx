import { useCallback, useLayoutEffect, useRef, useState } from "react";

import { DOCUMENT_ACCEPT } from "../api";
import { Icon } from "./Icon";
import { CloseIcon, FileIcon } from "./icons";
import type { ComposerAttachment } from "./useDocumentAttachments";

// Drop has no native `accept` filter (unlike the file input), so we filter the
// dropped files by the same extension list the picker advertises.
const ACCEPTED_EXTENSIONS = DOCUMENT_ACCEPT.split(",").map((ext) => ext.trim().toLowerCase());
function filterAcceptedFiles(files: File[]): File[] {
  return files.filter((file) => {
    const name = file.name.toLowerCase();
    return ACCEPTED_EXTENSIONS.some((ext) => name.endsWith(ext));
  });
}

// True only for OS file drags. Dragging selected text or a link also fires the
// drag events, but carries no "Files" type - we ignore those so the highlight
// doesn't flash for drags we can't attach.
function isFileDrag(event: { dataTransfer: DataTransfer }): boolean {
  return Array.from(event.dataTransfer.types).includes("Files");
}

export function Composer({
  variant,
  draft,
  isSending,
  sendDisabled = false,
  placeholder,
  autoFocus = false,
  onDraftChange,
  onSend,
  onStop,
  onAttachFiles,
  attachments = [],
  onRemoveAttachment,
}: {
  variant: "start" | "chat";
  draft: string;
  isSending: boolean;
  sendDisabled?: boolean;
  placeholder: string;
  autoFocus?: boolean;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
  // Invoked with the files the user picked from the native chooser. When omitted,
  // the attach button is disabled (e.g. before a thread exists).
  onAttachFiles?(files: File[]): void;
  attachments?: ComposerAttachment[];
  onRemoveAttachment?(id: string): void;
}) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  // Whether a drag is currently hovering the composer (drives the highlight).
  const [isDragging, setIsDragging] = useState(false);
  // dragenter/dragleave fire for every child element, so a plain boolean would
  // flicker. Count enters minus leaves; the highlight is on while depth > 0.
  const dragDepth = useRef(0);
  const dropEnabled = onAttachFiles !== undefined;
  // Base (empty) height per variant, preserved as the textarea's min-height so
  // the composer keeps its current look before any auto-grow kicks in.
  const textareaMinH = variant === "start" ? "min-h-[76px]" : "min-h-[56px]";
  const sendIconClass = variant === "chat" ? "h-4 w-4 -translate-y-px" : "h-4 w-4";
  const padX = "px-6";
  const canSend = !isSending && !sendDisabled && draft.trim() !== "";
  const actionButtonClass = isSending
    ? "bg-[#3a3a37] hover:bg-[#4b4a46]"
    : "bg-accent hover:bg-accent-strong disabled:bg-accent";
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  // Auto-grow: measure content height and apply it inline. The CSS max-height
  // caps the box; once content exceeds it, overflow-y-auto shows the scrollbar.
  // Direction of growth follows layout anchoring (the chat dock is sticky-bottom
  // -> grows upward; the start composer is top-anchored -> grows downward).
  const autoGrow = useCallback(() => {
    const el = textareaRef.current;
    if (el === null) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, []);
  // Re-measure on every draft change (typing, and reset to base after send).
  useLayoutEffect(autoGrow, [autoGrow, draft]);
  useLayoutEffect(() => {
    if (autoFocus) textareaRef.current?.focus();
  }, [autoFocus]);
  // Re-measure when the textarea's width changes (window resize, breakpoint,
  // rotation) - a different width re-wraps the text and changes the needed
  // height. Guard on width only: autoGrow mutates the element's height, so
  // reacting to height changes too would re-trigger the observer.
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (el === null) return;
    let lastWidth = el.clientWidth;
    const observer = new ResizeObserver(() => {
      if (el.clientWidth === lastWidth) return;
      lastWidth = el.clientWidth;
      autoGrow();
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [autoGrow]);
  return (
    <form
      className={`ui-composer relative flex flex-col rounded-[20px] border bg-[#2a2a28] shadow-[0_14px_24px_rgba(0,0,0,0.22)] transition-colors ${
        isDragging ? "border-accent bg-[#332f27]" : "border-[#4b4a46]"
      }`}
      onSubmit={(event) => {
        event.preventDefault();
        if (isSending) {
          onStop();
          return;
        }
        if (sendDisabled) return;
        onSend();
      }}
      onDragEnter={
        dropEnabled
          ? (event) => {
              if (!isFileDrag(event)) return;
              event.preventDefault();
              dragDepth.current += 1;
              setIsDragging(true);
            }
          : undefined
      }
      onDragOver={
        dropEnabled
          ? (event) => {
              if (!isFileDrag(event)) return;
              event.preventDefault();
              event.dataTransfer.dropEffect = "copy";
            }
          : undefined
      }
      onDragLeave={
        dropEnabled
          ? (event) => {
              if (!isFileDrag(event)) return;
              event.preventDefault();
              dragDepth.current = Math.max(0, dragDepth.current - 1);
              if (dragDepth.current === 0) setIsDragging(false);
            }
          : undefined
      }
      onDrop={
        dropEnabled
          ? (event) => {
              if (!isFileDrag(event)) return;
              event.preventDefault();
              dragDepth.current = 0;
              setIsDragging(false);
              const files = filterAcceptedFiles(Array.from(event.dataTransfer.files ?? []));
              if (files.length > 0) onAttachFiles?.(files);
            }
          : undefined
      }
    >
      {isDragging && (
        <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center rounded-[19px] bg-[#332f27]/92 text-sm font-medium text-[#f3f0e8]">
          {variant === "chat" ? "Drop files here to add to chat" : "Drop files here to attach"}
        </div>
      )}
      <textarea
        ref={textareaRef}
        className={`ui-composer-text ui-sidebar-scroll ${textareaMinH} w-full resize-none overflow-y-auto bg-transparent ${padX} pb-3 pt-5 text-[#f3f0e8] outline-none placeholder:text-[#aaa79e] max-h-[150px] md:max-h-[264px]`}
        placeholder={placeholder}
        value={draft}
        onChange={(event) => onDraftChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.shiftKey) {
            event.preventDefault();
            if (!isSending && !sendDisabled) onSend();
          }
        }}
      />
      {attachments.length > 0 && (
        <div className={`${padX} pb-3`}>
          <div className="flex flex-wrap gap-2">
            {attachments.map((attachment) => (
              <AttachmentPreview
                key={attachment.id}
                attachment={attachment}
                onRemove={onRemoveAttachment}
              />
            ))}
          </div>
        </div>
      )}
      <div className={`flex h-11 flex-none items-center justify-between ${padX} text-[#d8d4ca]`}>
        <button
          className="leading-none disabled:opacity-40"
          type="button"
          aria-label="Add attachment"
          disabled={onAttachFiles === undefined}
          onClick={() => fileInputRef.current?.click()}
        >
          <Icon name="plus" size="24px" />
        </button>
        <input
          ref={fileInputRef}
          type="file"
          multiple
          accept={DOCUMENT_ACCEPT}
          className="hidden"
          onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            // Reset so picking the same file again re-fires change.
            event.target.value = "";
            if (files.length > 0) onAttachFiles?.(files);
          }}
        />
        <div className="ui-meta-text flex items-center text-[#d8d4ca]">
          <button
            className={`ui-composer-send grid h-7 w-7 place-items-center rounded-md text-[#eeeae2] transition-colors disabled:cursor-not-allowed disabled:opacity-45 ${actionButtonClass}`}
            disabled={!isSending && !canSend}
            type="submit"
            aria-label={isSending ? "Stop response" : "Send message"}
          >
            {isSending ? (
              <svg className={sendIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="currentColor">
                <rect x="5.5" y="5.5" width="13" height="13" rx="2" />
              </svg>
            ) : (
              <svg className={sendIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="none">
                <path
                  d="M12 19V5M6.5 10.5 12 5l5.5 5.5"
                  stroke="currentColor"
                  strokeWidth="2.4"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            )}
          </button>
        </div>
      </div>
    </form>
  );
}

function AttachmentPreview({
  attachment,
  onRemove,
}: {
  attachment: ComposerAttachment;
  onRemove?: (id: string) => void;
}) {
  const status = attachmentStatusLabel(attachment);
  const uploading =
    attachment.status === "uploading" || attachment.status === "processing";
  return (
    <div className="group/attachment relative flex h-[76px] w-[180px] max-w-full overflow-hidden rounded-lg border border-[#4b4a46] bg-[#343432] text-[#f3f0e8] shadow-[0_8px_18px_rgba(0,0,0,0.18)]">
      <div className="grid h-full w-[68px] shrink-0 place-items-center bg-[#2f2f2c]">
        {attachment.previewUrl !== undefined ? (
          <img
            className="h-full w-full object-cover"
            src={attachment.previewUrl}
            alt=""
            aria-hidden="true"
          />
        ) : (
          <div className="grid h-10 w-10 place-items-center rounded-md border border-[#55534d] bg-[#292927] text-[#c9c5bb]">
            <FileIcon />
          </div>
        )}
      </div>
      <div className="min-w-0 flex-1 px-3 py-2">
        <div className="ui-message-text truncate text-sm">{attachment.filename}</div>
        <div className="ui-meta-text mt-1 truncate text-[#aaa79e]">
          {status}
        </div>
        {uploading && (
          <div className="mt-2 h-1 overflow-hidden rounded-full bg-[#232321]">
            <div className="h-full w-1/2 animate-[attachment-progress_1.1s_ease-in-out_infinite] rounded-full bg-accent" />
          </div>
        )}
      </div>
      {onRemove !== undefined && (
        <button
          className="absolute left-1 top-1 grid h-5 w-5 place-items-center rounded-full border border-[#64615a] bg-[#343432] text-[#d8d4ca] opacity-95 transition-colors hover:bg-[#44423d] hover:text-[#f3f0e8]"
          type="button"
          aria-label={`Remove ${attachment.filename}`}
          onClick={() => onRemove(attachment.id)}
        >
          <CloseIcon />
        </button>
      )}
    </div>
  );
}

function attachmentStatusLabel(attachment: ComposerAttachment): string {
  if (attachment.status === "queued") return "Attached";
  if (attachment.status === "uploading") return "Uploading...";
  if (attachment.status === "processing") return "Processing...";
  if (attachment.status === "ready") return formatBytes(attachment.sizeBytes);
  return attachment.error ?? "Upload failed";
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb.toFixed(kb >= 10 ? 0 : 1)} KB`;
  const mb = kb / 1024;
  return `${mb.toFixed(mb >= 10 ? 0 : 1)} MB`;
}
