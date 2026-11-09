import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { Composer } from "./Composer";
import { ErrorText } from "./ErrorText";
import { Icon } from "./Icon";
import { pickGreeting } from "./threadUtils";
import { PromptStarters } from "./PromptStarters";
import type { PastedText } from "./pastedText";
import type { ComposerAttachment } from "./useDocumentAttachments";
import { WindowFileDrop } from "./WindowFileDrop";
import loomLogo from "../assets/loom-logo.svg";
import { TodaySection } from "../today/TodaySection";
import { useToday } from "../today/useToday";

export function StartPanel({
  displayName,
  draft,
  sendError,
  attachments,
  attachNote,
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
  onOpenThread,
  onOpenMemory,
}: {
  displayName: string;
  draft: string;
  sendError: string;
  attachments: ComposerAttachment[];
  attachNote: string;
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
  onOpenThread(threadID: string): void;
  onOpenMemory(): void;
}) {
  const { t } = useTranslation();
  const today = useToday();
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
  // The starters fill the page only when Today has nothing to show: not
  // while it loads (no flash), but when it failed.
  const todayState = today.state;
  const todayQuiet =
    todayState.status === "error" ||
    (todayState.status === "ready" &&
      todayState.today.weather === null &&
      todayState.today.runs.length === 0);
  return (
    <section className="@container flex h-svh min-h-0 flex-col">
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
      {/* The top gap centers the header, greeting, composer and weather
          tiles in the screen: ~596px side by side, ~812px stacked (a column
          under 596px), at least 2rem, at most 22.7svh. It follows the screen
          only: tied to the content, the composer jumped when Today loaded, a
          line was typed or a run was read. A fixed 22.7vh left a tablet
          scrolling with a fifth of it empty above. */}
      <div className="flex min-h-0 flex-1 flex-col items-center justify-start overflow-y-auto px-4 pt-[clamp(2rem,calc((100svh_-_596px)/2),22.7svh)] pb-12 sm:px-8 @max-[596px]:pt-[clamp(2rem,calc((100svh_-_812px)/2),22.7svh)]">
        <h2 className="ui-greeting-text mb-8 text-center font-serif">
          <img
            src={loomLogo}
            alt=""
            aria-hidden
            className="mr-2.5 inline-block h-10 w-10 align-middle"
          />
          {greeting}
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
            // Never "sending": a turn started here moves to its own thread, and
            // the start screen stays free for the next one.
            isSending={false}
            placeholder={t("startPanel.placeholder")}
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
          {todayQuiet && draft.trim() === "" && attachments.length === 0 && (
            <PromptStarters onPick={onDraftChange} />
          )}
          {todayState.status === "ready" && (
            <TodaySection
              onMarkAllRead={today.markAllRead}
              onMarkRead={today.markRead}
              onOpenMemory={onOpenMemory}
              onOpenThread={onOpenThread}
              onReload={() => void today.reload()}
              today={todayState.today}
            />
          )}
        </div>
      </div>
    </section>
  );
}
