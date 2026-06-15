import type { McpStatusEvent } from "../api";
import logoImage from "../assets/mynd-logo.png";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { Composer } from "./Composer";
import { ErrorText } from "./ErrorText";
import { greetingForNow } from "./chatUtils";
import { McpStatusIndicator } from "./SidebarItems";
import type { ComposerAttachment } from "./useDocumentAttachments";
import { WindowFileDrop } from "./WindowFileDrop";

export function StartPanel({
  displayName,
  draft,
  isSending,
  sendDisabled,
  mcpStatus,
  sendError,
  attachments,
  attachNote,
  onOpenSidebar,
  onDraftChange,
  onSend,
  onStop,
  onAttachFiles,
  onAttachError,
  onRemoveAttachment,
}: {
  displayName: string;
  draft: string;
  isSending: boolean;
  sendDisabled: boolean;
  mcpStatus: McpStatusEvent | null;
  sendError: string;
  attachments: ComposerAttachment[];
  attachNote: string;
  onOpenSidebar(): void;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
  onAttachFiles(files: File[]): void;
  onAttachError(message: string): void;
  onRemoveAttachment(id: string): void;
}) {
  // No thread exists yet, so uploads are deferred: files are held (see
  // pendingAttachmentNames) and bound to the chat once the first send creates it.
  return (
    <section className="flex h-svh min-h-0 flex-col">
      <header
        aria-label="Chat header"
        className="ui-control-text flex h-9 shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 text-[#d5d2c9]"
        role="banner"
      >
        <div className="flex min-w-0 items-center gap-2">
          <SidebarOpenButton onClick={onOpenSidebar} />
        </div>
        {mcpStatus !== null && mcpStatus.configured > 0 && <McpStatusIndicator compact status={mcpStatus} />}
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-4 pb-[14vh] sm:px-8">
        <h2 className="ui-greeting-text mb-8 flex items-center gap-4 font-serif">
          <img className="h-16 w-auto shrink-0 -translate-y-1" src={logoImage} alt="" aria-hidden="true" />
          {greetingForNow(displayName)}
        </h2>
        <div className="w-full max-w-[674px]">
          <WindowFileDrop enabled onAttachFiles={onAttachFiles} onAttachError={onAttachError} />
          <Composer
            variant="start"
            autoFocus
            draft={draft}
            isSending={isSending}
            sendDisabled={sendDisabled}
            placeholder="How can I help you today?"
            onDraftChange={onDraftChange}
            onSend={onSend}
            onStop={onStop}
            onAttachFiles={onAttachFiles}
            onAttachError={onAttachError}
            attachments={attachments}
            onRemoveAttachment={onRemoveAttachment}
          />
          {attachNote !== "" && (
            <div className="ui-meta-text mt-2 text-center text-[#858178]">{attachNote}</div>
          )}
          {sendError !== "" && <ErrorText>{sendError}</ErrorText>}
          <div className="ui-meta-text mt-4 flex flex-wrap justify-center gap-2 text-[#e8e4da]">
            <PromptChip icon="◇" label="Write" />
            <PromptChip icon="▱" label="Learn" />
            <PromptChip icon="‹/›" label="Code" />
            <PromptChip icon="☕" label="Life stuff" />
            <PromptChip icon="◌" label="Lume's choice" />
          </div>
        </div>
      </div>
    </section>
  );
}

function PromptChip({ icon, label }: { icon: string; label: string }) {
  return (
    <button className="ui-meta-text flex h-8 items-center gap-1.5 rounded-lg bg-[#3a3a37] px-3 text-[#eeeae2]" type="button">
      <span className="text-[#aaa79e]">{icon}</span>
      {label}
    </button>
  );
}
