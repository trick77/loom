import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { ATTACHMENT_ACCEPT } from "../api";
import i18n from "../i18n";
import { AttachmentPreview } from "../components/AttachmentPreview";
import { attachAcceptedFiles, formatAttachmentSize } from "./attachmentFiles";
import { Icon } from "./Icon";
import { ReasoningMenu } from "./ReasoningMenu";
import { MODEL_LABEL, type ReasoningEffort } from "./reasoning";
import { matchSlashCommand, slashSuggestions } from "./slashCommands";
import { PASTE_AS_ATTACHMENT_THRESHOLD, type PastedText } from "./pastedText";
import { isImageAttachment, type ComposerAttachment } from "./useDocumentAttachments";

export function Composer({
  variant,
  draft,
  isSending,
  sendDisabled = false,
  placeholder,
  autoFocus = false,
  incognito = false,
  reasoningEffort,
  onReasoningEffortChange,
  onDraftChange,
  onSend,
  onStop,
  onAttachFiles,
  onAttachError,
  attachments = [],
  onRemoveAttachment,
  pastedTexts = [],
  onAddPastedText,
  onRemovePastedText,
}: {
  variant: "start" | "thread";
  draft: string;
  isSending: boolean;
  sendDisabled?: boolean;
  placeholder: string;
  autoFocus?: boolean;
  // Incognito mode: dashed outline and no attachment affordance (uploads persist,
  // so they are unavailable in an ephemeral thread).
  incognito?: boolean;
  // The reasoning-effort selection and its setter, shown in the footer next to the
  // static model label.
  reasoningEffort: ReasoningEffort;
  onReasoningEffortChange(value: ReasoningEffort): void;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
  // Invoked with the files the user picked from the native chooser. When omitted,
  // the attach button is disabled (e.g. before a thread exists).
  onAttachFiles?(files: File[]): void;
  onAttachError?(message: string): void;
  attachments?: ComposerAttachment[];
  onRemoveAttachment?(id: string): void;
  // Large pastes collapsed into removable "Pasted" chips (see pastedText.ts).
  // When onAddPastedText is omitted, an oversized paste falls back to inline insertion.
  pastedTexts?: PastedText[];
  onAddPastedText?(text: string): void;
  onRemovePastedText?(id: string): void;
}) {
  const { t } = useTranslation();
  const fileInputRef = useRef<HTMLInputElement>(null);
  // Slash-command typeahead: while the draft is a lone "/token", suggest matching
  // commands. `dismissed` hides the popover after Escape until the draft changes.
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [suggestionsDismissed, setSuggestionsDismissed] = useState(false);
  const suggestions = suggestionsDismissed ? [] : slashSuggestions(draft);
  const showSuggestions = suggestions.length > 0;
  // `suggestions` recomputes synchronously from the draft, but selectedIndex is
  // only re-clamped by the effect below (which runs after paint). During the
  // render right after the draft narrows the list, selectedIndex can still point
  // past the end, so derive a clamped index for reads — keyboard completion and
  // the highlight must never index an undefined suggestion.
  const activeIndex = selectedIndex < suggestions.length ? selectedIndex : 0;
  useEffect(() => {
    // Keep the highlighted item in range as the draft narrows the matches.
    setSelectedIndex((current) => (current >= suggestions.length ? 0 : current));
  }, [suggestions.length]);
  // Base (empty) height per variant, preserved as the textarea's min-height so
  // the composer keeps its current look before any auto-grow kicks in.
  const textareaMinH = variant === "start" ? "min-h-[76px]" : "min-h-[56px]";
  const sendIconClass = variant === "thread" ? "h-4 w-4 -translate-y-px" : "h-4 w-4";
  const padX = "px-6";
  // A paste-only message (empty textarea but a staged "Pasted" chip) is sendable.
  const hasContent = draft.trim() !== "" || pastedTexts.length > 0;
  const hasStagedRow = attachments.length > 0 || pastedTexts.length > 0;
  const canSend = !isSending && !sendDisabled && hasContent;
  const actionButtonClass = isSending
    ? "bg-[#3a3a37] hover:bg-[#4b4a46]"
    : "bg-accent hover:bg-accent-strong disabled:bg-accent";
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  // Auto-grow: measure content height and apply it inline. The CSS max-height
  // caps the box; once content exceeds it, overflow-y-auto shows the scrollbar.
  // Direction of growth follows layout anchoring (the thread dock is sticky-bottom
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
  const attachFiles = useCallback(
    (files: File[]) => {
      attachAcceptedFiles({ files, onAttachFiles, onAttachError });
    },
    [onAttachError, onAttachFiles],
  );
  return (
    <form
      className={`ui-composer relative flex flex-col rounded-[20px] border ${incognito ? "border-dashed border-[#5a5852]" : "border-[#4b4a46]"} bg-[#2a2a28] shadow-[0_14px_24px_rgba(0,0,0,0.22)]`}
      onSubmit={(event) => {
        event.preventDefault();
        if (isSending) {
          onStop();
          return;
        }
        if (sendDisabled) return;
        onSend();
      }}
    >
      {hasStagedRow && (
        <div
          aria-label={t("composer.attachments")}
          className={`ui-sidebar-scroll ${padX} flex-none overflow-y-auto pt-5 pb-2 max-h-[104px]`}
        >
          <div className="flex flex-wrap gap-2">
            {pastedTexts.map((pasted) => (
              <PastedTextPill key={pasted.id} pasted={pasted} onRemove={onRemovePastedText} />
            ))}
            {attachments.map((attachment) => (
              <AttachmentPill
                key={attachment.id}
                attachment={attachment}
                onRemove={onRemoveAttachment}
              />
            ))}
          </div>
        </div>
      )}
      <textarea
        ref={textareaRef}
        rows={1}
        className={`ui-composer-text ui-sidebar-scroll ${textareaMinH} w-full resize-none overflow-y-auto bg-transparent ${padX} ${hasStagedRow ? "pt-2" : "pt-5"} pb-3 text-[#f3f0e8] outline-none placeholder:text-[#aaa79e] max-h-[150px] md:max-h-[264px]`}
        placeholder={placeholder}
        value={draft}
        onPaste={(event) => {
          // A very large plain-text paste collapses into a removable "Pasted" chip
          // instead of flooding the textarea. Char count only — matches claude.ai.
          if (onAddPastedText === undefined) return;
          const text = event.clipboardData.getData("text/plain");
          if (text.length <= PASTE_AS_ATTACHMENT_THRESHOLD) return;
          event.preventDefault();
          onAddPastedText(text);
        }}
        onChange={(event) => {
          // Any edit re-opens the popover a prior Escape had dismissed.
          setSuggestionsDismissed(false);
          onDraftChange(event.target.value);
        }}
        onKeyDown={(event) => {
          if (showSuggestions) {
            if (event.key === "ArrowDown") {
              event.preventDefault();
              setSelectedIndex((current) => (current + 1) % suggestions.length);
              return;
            }
            if (event.key === "ArrowUp") {
              event.preventDefault();
              setSelectedIndex((current) => (current - 1 + suggestions.length) % suggestions.length);
              return;
            }
            if (event.key === "Escape") {
              event.preventDefault();
              setSuggestionsDismissed(true);
              return;
            }
            if (event.key === "Tab") {
              event.preventDefault();
              onDraftChange(`/${suggestions[activeIndex].name}`);
              return;
            }
            if (event.key === "Enter" && !event.shiftKey) {
              event.preventDefault();
              // A fully typed command submits (runSlashCommand fires downstream);
              // a partial one first completes to the highlighted command.
              if (matchSlashCommand(draft) !== null) {
                if (!isSending && !sendDisabled) onSend();
              } else {
                onDraftChange(`/${suggestions[activeIndex].name}`);
              }
              return;
            }
          }
          if (event.key === "Enter" && !event.shiftKey) {
            event.preventDefault();
            if (!isSending && !sendDisabled) onSend();
          }
        }}
      />
      {showSuggestions && (
        <div className="absolute bottom-full left-3 z-10 mb-2 w-[280px] overflow-hidden rounded-xl border border-[#4b4a46] bg-[#2f2f2c] py-1 shadow-[0_14px_28px_rgba(0,0,0,0.32)]">
          {suggestions.map((command, index) => (
            <button
              key={command.name}
              type="button"
              // Prevent the textarea from losing focus (which would fire before onClick).
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => onDraftChange(`/${command.name}`)}
              onMouseEnter={() => setSelectedIndex(index)}
              className={`flex w-full items-baseline gap-2 px-3 py-1.5 text-left ${
                index === activeIndex ? "bg-[#3a3a37]" : ""
              }`}
            >
              <span className="font-mono text-sm text-[#f3f0e8]">/{command.name}</span>
              <span className="truncate text-xs text-[#aaa79e]">{t(command.description)}</span>
            </button>
          ))}
        </div>
      )}
      <div className={`flex h-11 flex-none items-center justify-between ${padX} text-[#d8d4ca]`}>
        {incognito ? (
          // Uploads persist (indexing / artifact rows), so they are unavailable in an
          // ephemeral incognito thread — the attach affordance is omitted entirely. The
          // empty span preserves the send button's right-alignment.
          <span aria-hidden />
        ) : (
          <button
            className="leading-none disabled:opacity-40"
            type="button"
            aria-label={t("composer.addAttachment")}
            disabled={onAttachFiles === undefined}
            onClick={() => fileInputRef.current?.click()}
          >
            <Icon name="plus" size="24px" />
          </button>
        )}
        <input
          ref={fileInputRef}
          type="file"
          multiple
          accept={ATTACHMENT_ACCEPT}
          className="hidden"
          onChange={(event) => {
            const files = Array.from(event.target.files ?? []);
            // Reset so picking the same file again re-fires change.
            event.target.value = "";
            if (files.length > 0) attachFiles(files);
          }}
        />
        <div className="ui-meta-text flex items-center text-[#d8d4ca]">
          {/* Static model label — Loom serves one model, so this is a name, not a
              picker. Hidden on the narrowest widths so the reasoning control and
              send button always fit. No trailing gap here: the reasoning trigger's
              own left padding is the single word-space before it. */}
          <span className="hidden select-none text-[13px] leading-none text-[#aaa79e] sm:inline">{MODEL_LABEL}</span>
          <ReasoningMenu value={reasoningEffort} onChange={onReasoningEffortChange} />
          <button
            className={`ui-composer-send ml-2 grid h-7 w-7 place-items-center rounded-md text-[#eeeae2] transition-colors disabled:cursor-not-allowed disabled:opacity-45 ${actionButtonClass}`}
            disabled={!isSending && !canSend}
            type="submit"
            aria-label={isSending ? t("composer.stopResponse") : t("composer.sendMessage")}
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

function AttachmentRemoveButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      className="absolute left-1 top-1 grid h-5 w-5 place-items-center rounded-full border border-[#64615a] bg-[#343432] text-[#d8d4ca] opacity-95 transition-colors hover:bg-[#44423d] hover:text-[#f3f0e8]"
      type="button"
      aria-label={label}
      onClick={onClick}
    >
      <Icon name="closeCircle" size="14px" />
    </button>
  );
}

// A large paste, rendered as a compact card echoing claude.ai's "Pasted text"
// chip: a small clamped preview of the raw text with an uppercase "Pasted" badge.
function PastedTextPill({ pasted, onRemove }: { pasted: PastedText; onRemove?: (id: string) => void }) {
  const { t } = useTranslation();
  return (
    <div
      className="group/attachment relative flex h-[76px] w-[140px] max-w-full flex-col overflow-hidden rounded-lg border border-[#4b4a46] bg-[#343432] text-[#f3f0e8] shadow-[0_8px_18px_rgba(0,0,0,0.18)]"
      title={t("composer.pastedText")}
    >
      {/* Preview of the raw text: tiny, wrapped, clipped by the fixed card height.
          Cap the substring so a multi-megabyte paste never renders in full. */}
      <p className="min-h-0 flex-1 overflow-hidden whitespace-pre-wrap break-all px-2 pt-2 pl-7 font-mono text-[8px] leading-[11px] text-[#aaa79e]">
        {pasted.text.slice(0, 400)}
      </p>
      <div className="flex-none px-2 pb-1.5">
        <span className="inline-block rounded-[4px] border border-[#55534d] bg-[#2f2f2c]/70 px-1 py-0.5 text-[10px] font-medium uppercase leading-none text-[#d8d4ca]">
          {t("composer.pastedBadge")}
        </span>
      </div>
      {onRemove !== undefined && (
        <AttachmentRemoveButton
          label={t("composer.removePastedText", { count: pasted.lineCount })}
          onClick={() => onRemove(pasted.id)}
        />
      )}
    </div>
  );
}

function AttachmentPill({
  attachment,
  onRemove,
}: {
  attachment: ComposerAttachment;
  onRemove?: (id: string) => void;
}) {
  const { t } = useTranslation();
  const uploading =
    attachment.status === "uploading" || attachment.status === "processing";

  // An uploaded image shows as a compact thumbnail with its type as a pill badge
  // inside the image — identical to how it renders once sent (messages.tsx). Files,
  // which have no thumbnail, keep the wider card so the filename stays readable.
  if (isImageAttachment(attachment)) {
    return (
      <div
        className="group/attachment relative h-[76px] w-[76px] overflow-hidden rounded-lg border border-[#4b4a46] bg-[#2f2f2c] shadow-[0_8px_18px_rgba(0,0,0,0.18)]"
        title={attachment.filename}
      >
        <AttachmentPreview
          mimeType={attachment.mimeType}
          filename={attachment.filename}
          previewUrl={attachment.previewUrl}
          overlayLabel
          className="h-full w-full"
        />
        {uploading && (
          <span className="absolute inset-0 grid place-items-center bg-black/40">
            <Icon name="spinner" size="20px" className="text-[#f3f0e8]" />
          </span>
        )}
        {attachment.status === "error" && (
          <span className="absolute inset-x-0 bottom-0 truncate bg-[#5a2a27]/90 px-1.5 py-0.5 text-center text-[11px] text-[#f3d6d2]">
            {attachment.error ?? t("composer.uploadFailed")}
          </span>
        )}
        {onRemove !== undefined && (
          <AttachmentRemoveButton
            label={t("composer.removeAttachment", { filename: attachment.filename })}
            onClick={() => onRemove(attachment.id)}
          />
        )}
      </div>
    );
  }

  const status = attachmentStatusLabel(attachment);
  return (
    <div className="group/attachment relative flex h-[76px] w-[180px] max-w-full overflow-hidden rounded-lg border border-[#4b4a46] bg-[#343432] text-[#f3f0e8] shadow-[0_8px_18px_rgba(0,0,0,0.18)]">
      <AttachmentPreview
        mimeType={attachment.mimeType}
        filename={attachment.filename}
        previewUrl={attachment.previewUrl}
        className="grid h-full w-[68px] shrink-0 place-items-center bg-[#2f2f2c]"
        fallbackBoxClassName="grid h-10 w-10 place-items-center rounded-md border border-[#55534d] bg-[#292927]"
      />
      <div className="min-w-0 flex-1 px-3 py-2">
        <div className="ui-message-text truncate">{attachment.filename}</div>
        <div className="ui-meta-text mt-2 truncate text-[#aaa79e]">
          {status}
        </div>
        {uploading && (
          <div className="mt-2 h-1 overflow-hidden rounded-full bg-[#232321]">
            <div className="h-full w-1/2 animate-[attachment-progress_1.1s_ease-in-out_infinite] rounded-full bg-accent" />
          </div>
        )}
      </div>
      {onRemove !== undefined && (
        <AttachmentRemoveButton
          label={t("composer.removeAttachment", { filename: attachment.filename })}
          onClick={() => onRemove(attachment.id)}
        />
      )}
    </div>
  );
}

function attachmentStatusLabel(attachment: ComposerAttachment): string {
  if (attachment.status === "queued") return i18n.t("composer.attached");
  if (attachment.status === "uploading") return i18n.t("composer.uploading");
  if (attachment.status === "processing") return i18n.t("composer.processing");
  if (attachment.status === "ready") return formatAttachmentSize(attachment.sizeBytes);
  return attachment.error ?? i18n.t("composer.uploadFailed");
}
