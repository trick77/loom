import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Idea, Project } from "../api";
import { ideaThreadDraft } from "../ideas/ideaText";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { Composer } from "./Composer";
import { ErrorText } from "./ErrorText";
import { Icon } from "./Icon";
import { readGreetingVisit, rememberGreetingVisit } from "./greetingVisit";
import { pickGreeting } from "./threadUtils";
import { PromptStarters } from "./PromptStarters";
import { StartProjectPicker } from "./StartProjectPicker";
import type { PastedText } from "./pastedText";
import type { ComposerAttachment } from "./useDocumentAttachments";
import { WindowFileDrop } from "./WindowFileDrop";
import loomLogo from "../assets/loom-logo.svg";
import { TodaySection } from "../today/TodaySection";
import { useToday } from "../today/useToday";

export function StartPanel({
  userID,
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
  projects,
  selectedProject,
  onSelectProject,
  onCreateProject,
  onSessionExpired,
  onOpenIdea,
  onStartIdeaThread,
}: {
  userID: string;
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
  /** Follow-up cards: open the idea, or fill the composer with it. */
  onOpenIdea?(ideaID: string): void;
  onStartIdeaThread?(text: string): void;
  /** Active projects the new chat can start in. */
  projects: Project[];
  selectedProject: Project | null;
  onSelectProject(projectID: string | null): void;
  onCreateProject(): void;
  onSessionExpired(): void;
}) {
  const { t } = useTranslation();
  const today = useToday();
  // No thread exists yet, so uploads are deferred: files are held (see
  // pendingAttachmentNames) and bound to the thread once the first send creates it.
  // Pick the greeting slot once per mount (useState keeps the lazy initialiser's
  // result, so the random choice never re-rolls on re-render), but translate it
  // below at render time — so switching the UI language re-localizes the greeting
  // instead of leaving the mount-time language frozen in.
  // The visit is only read here; StrictMode calls this initialiser twice, so a
  // write in it would spend the day's named greeting before anyone saw it.
  const [greetingPick] = useState(() => {
    const now = new Date();
    const visit = readGreetingVisit(userID, now);
    return pickGreeting(displayName, now, Math.random, visit);
  });
  // Record the visit once the screen is actually shown: a tab restored in the
  // background must not spend the named greeting.
  useEffect(() => {
    const remember = () => {
      if (document.visibilityState !== "visible") return false;
      rememberGreetingVisit(userID, new Date());
      return true;
    };
    if (remember()) return;
    const onVisible = () => {
      if (remember()) {
        document.removeEventListener("visibilitychange", onVisible);
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [userID]);
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
      todayState.today.runs.length === 0 &&
      todayState.today.ideas.length === 0);
  const ideaActions =
    onOpenIdea === undefined || onStartIdeaThread === undefined
      ? undefined
      : {
          onOpenIdea,
          onStartThread: (idea: Idea) =>
            onStartIdeaThread(ideaThreadDraft(idea, t)),
          onHide: (ideaID: string) =>
            today.settleIdea(
              ideaID,
              { remindDismissed: true },
              onSessionExpired,
            ),
          onDone: (ideaID: string) =>
            today.settleIdea(ideaID, { done: true }, onSessionExpired),
        };
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
        <h2 className="ui-greeting-text mb-6 text-center font-serif">
          <img
            src={loomLogo}
            alt=""
            aria-hidden
            className="ui-greeting-logo mr-2.5 inline-block h-8 w-8"
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
          <div className="mt-2 flex">
            <StartProjectPicker
              projects={projects}
              selectedProject={selectedProject}
              onSelect={onSelectProject}
              onCreateProject={onCreateProject}
            />
          </div>
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
              onOpenThread={onOpenThread}
              onSessionExpired={onSessionExpired}
              onReload={() => void today.reload()}
              today={todayState.today}
              ideaActions={ideaActions}
            />
          )}
        </div>
      </div>
    </section>
  );
}
