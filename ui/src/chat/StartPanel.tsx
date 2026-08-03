import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { Composer } from "./Composer";
import { ErrorText } from "./ErrorText";
import { Icon } from "./Icon";
import { pickGreeting } from "./threadUtils";
import { PromptStarters } from "./PromptStarters";
import type { PastedText } from "./pastedText";
import type { ReasoningEffort } from "./reasoning";
import type { ComposerAttachment } from "./useDocumentAttachments";
import { WindowFileDrop } from "./WindowFileDrop";
import loomLogo from "../assets/loom-logo.svg";

export function StartPanel({
  displayName,
  draft,
  isSending,
  sendDisabled,
  sendError,
  attachments,
  attachNote,
  reasoningEffort,
  onReasoningEffortChange,
  onOpenSidebar,
  onDraftChange,
  pastedTexts,
  onAddPastedText,
  onRemovePastedText,
  onSend,
  onStop,
  onAttachFiles,
  onAttachError,
  onRemoveAttachment,
  onEnterIncognito,
}: {
  displayName: string;
  draft: string;
  isSending: boolean;
  sendDisabled: boolean;
  sendError: string;
  attachments: ComposerAttachment[];
  attachNote: string;
  reasoningEffort: ReasoningEffort;
  onReasoningEffortChange(value: ReasoningEffort): void;
  onOpenSidebar(): void;
  onDraftChange(value: string): void;
  pastedTexts: PastedText[];
  onAddPastedText(text: string): void;
  onRemovePastedText(id: string): void;
  onSend(): void;
  onStop(): void;
  onAttachFiles(files: File[]): void;
  onAttachError(message: string): void;
  onRemoveAttachment(id: string): void;
  onEnterIncognito(): void;
}) {
  const { t } = useTranslation();
  // No thread exists yet, so uploads are deferred: files are held (see
  // pendingAttachmentNames) and bound to the thread once the first send creates it.
  // Pick the greeting slot once per mount (useState's lazy initialiser runs exactly
  // once, so the random choice never re-rolls on re-render), but translate it below
  // at render time — so switching the UI language re-localizes the greeting instead
  // of leaving the mount-time language frozen in.
  const [greetingPick] = useState(() => pickGreeting(displayName));
  const greeting = t(`greetings.${greetingPick.key}.${greetingPick.form}`, {
    name: greetingPick.name,
  });
  return (
    <section className="flex h-svh min-h-0 flex-col">
      <header
        aria-label={t("thread.header")}
        className="ui-control-text flex h-9 shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 text-[#d5d2c9]"
        role="banner"
      >
        <div className="flex min-w-0 items-center gap-2">
          <SidebarOpenButton onClick={onOpenSidebar} />
        </div>
        <button
          aria-label={t("startPanel.useIncognito")}
          className="grid h-8 w-8 place-items-center rounded-md text-[#d5d2c9] transition-colors hover:bg-[#2a2a28] hover:text-[#f3f0e8]"
          onClick={onEnterIncognito}
          title={t("startPanel.useIncognito")}
          type="button"
        >
          <Icon name="ghost" size="18px" />
        </button>
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-start overflow-y-auto px-4 pt-[22.7vh] sm:px-8">
        <h2 className="ui-greeting-text mb-8 flex flex-col items-center gap-2 text-center font-serif sm:flex-row sm:gap-2.5 sm:text-left">
          <img
            src={loomLogo}
            alt=""
            aria-hidden
            className="h-10 w-10 sm:-translate-y-1"
          />
          <span className="sm:-translate-y-0.5">{greeting}</span>
        </h2>
        <div className="w-full max-w-[674px]">
          <WindowFileDrop
            enabled
            onAttachFiles={onAttachFiles}
            onAttachError={onAttachError}
          />
          <Composer
            variant="start"
            autoFocus
            draft={draft}
            isSending={isSending}
            sendDisabled={sendDisabled}
            placeholder={t("startPanel.placeholder")}
            reasoningEffort={reasoningEffort}
            onReasoningEffortChange={onReasoningEffortChange}
            onDraftChange={onDraftChange}
            pastedTexts={pastedTexts}
            onAddPastedText={onAddPastedText}
            onRemovePastedText={onRemovePastedText}
            onSend={onSend}
            onStop={onStop}
            onAttachFiles={onAttachFiles}
            onAttachError={onAttachError}
            attachments={attachments}
            onRemoveAttachment={onRemoveAttachment}
          />
          {attachNote !== "" && (
            <div className="ui-meta-text mt-2 text-center text-[#858178]">
              {attachNote}
            </div>
          )}
          {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
          {/* Hide the generic prompt starters once an attachment is staged (e.g.
              "Use in thread" pre-attaches an artifact) — they don't apply when the
              user is already working from a specific file. */}
          {draft.trim() === "" && attachments.length === 0 && (
            <PromptStarters onPick={onDraftChange} />
          )}
        </div>
      </div>
    </section>
  );
}
