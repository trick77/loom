import type { McpStatusEvent } from "../api";
import logoImage from "../assets/loom-logo.png";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { Composer } from "./Composer";
import { ErrorText } from "./ErrorText";
import { greetingForNow } from "./threadUtils";
import { PromptStarters } from "./PromptStarters";
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
  // pendingAttachmentNames) and bound to the thread once the first send creates it.
  return (
    <section className="flex h-svh min-h-0 flex-col">
      <header
        aria-label="Thread header"
        className="ui-control-text flex h-9 shrink-0 items-center justify-between gap-3 border-b border-[#252523] px-4 text-[#d5d2c9]"
        role="banner"
      >
        <div className="flex min-w-0 items-center gap-2">
          <SidebarOpenButton onClick={onOpenSidebar} />
        </div>
        {mcpStatus !== null && mcpStatus.configured > 0 && <McpStatusIndicator compact status={mcpStatus} />}
      </header>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-start overflow-y-auto px-4 pt-[22.7vh] sm:px-8">
        <h2 className="ui-greeting-text mb-8 flex items-center gap-2 font-serif">
          <img className="h-12 w-auto shrink-0 -translate-y-1" src={logoImage} alt="" aria-hidden="true" />
          <span className="-translate-y-0.5">{greetingForNow(displayName)}</span>
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
          {draft.trim() === "" && <PromptStarters onPick={onDraftChange} />}
        </div>
      </div>
    </section>
  );
}
