import { useEffect, useState } from "react";

import type { Project, Thread } from "../api";
import { Composer } from "../chat/Composer";
import {
  isImageAttachment,
  toSentAttachment,
  useDocumentAttachments,
  type ComposerAttachment,
} from "../chat/useDocumentAttachments";
import { WindowFileDrop } from "../chat/WindowFileDrop";
import { Icon } from "../chat/Icon";
import { ChatRow } from "../chats/ChatRow";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { ProjectActionsMenu } from "./ProjectActionsMenu";
import { ProjectMemoryPanel } from "./ProjectMemoryPanel";
import { ProjectKnowledgePanel } from "./ProjectKnowledgePanel";

export function ProjectDetailPage({
  project,
  threads,
  draft,
  sendError,
  isSending,
  sendDisabled = false,
  openThreadMenuID,
  onBack,
  onDraftChange,
  onSend,
  onStop,
  onOpenThread,
  onRenameThread,
  onDeleteThread,
  onStarThread,
  onRemoveFromProject,
  onToggleThreadMenu,
  onCloseThreadMenu,
  onEditProject,
  onArchiveProject,
  onUnarchiveProject,
  onDeleteProject,
  onToggleStar,
  onOpenSidebar,
}: {
  project: Project;
  threads: Thread[];
  draft: string;
  sendError: string;
  isSending: boolean;
  sendDisabled?: boolean;
  openThreadMenuID: string | null;
  onBack(): void;
  onDraftChange(value: string): void;
  onSend(attachments?: ComposerAttachment[]): void;
  onStop(): void;
  onOpenThread(threadID: string): void;
  onRenameThread(thread: Thread): void;
  onDeleteThread(thread: Thread): void;
  onStarThread(thread: Thread, starred: boolean, menuKey: string): void;
  onRemoveFromProject(thread: Thread): void;
  onToggleThreadMenu(menuKey: string): void;
  onCloseThreadMenu(): void;
  onEditProject(project: Project): void;
  onArchiveProject(project: Project): void;
  onUnarchiveProject(project: Project): void;
  onDeleteProject(project: Project): void;
  onToggleStar(project: Project, starred: boolean): void;
  onOpenSidebar(): void;
}) {
  const projectMenuKey = `Project:${project.id}`;
  const [hoveredThreadID, setHoveredThreadID] = useState<string | null>(null);
  // Composer uploads are scoped to this project's knowledge.
  const { attachNote, attachments, clearAttachments, handleAttachError, handleAttachFiles, removeAttachment } =
    useDocumentAttachments({
      projectId: project.id,
    });
  const imageUploadPending = attachments.some(
    (attachment) => isImageAttachment(attachment) && attachment.artifactId === undefined && attachment.status !== "error",
  );
  const handleSendRequest = () => {
    const sentAttachments = attachments.map(toSentAttachment);
    if (sentAttachments.length > 0) clearAttachments({ revokePreviewUrls: false });
    onSend(sentAttachments);
  };

  useEffect(() => {
    if (openThreadMenuID !== projectMenuKey) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Element)) return;
      if (target.closest("[data-project-detail-menu-root]") !== null) return;
      onCloseThreadMenu();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [onCloseThreadMenu, openThreadMenuID, projectMenuKey]);

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-[1080px] px-4 pb-16 pt-10 md:px-6">
        <button
          aria-label="All projects"
          className="ui-control-text flex items-center gap-2 text-[#c7c5bd] hover:text-white"
          type="button"
          onClick={onBack}
        >
          &larr; All projects
        </button>
        <div className="mt-2 flex flex-col gap-8 lg:flex-row lg:items-start">
          <div className="min-w-0 w-full lg:max-w-[674px]">
            <header className="mt-5 flex items-start justify-between gap-4">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <SidebarOpenButton onClick={onOpenSidebar} />
                  <h1 className="truncate font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">
                    {project.name}
                  </h1>
                </div>
                {project.description !== "" && (
                  <p className="mt-3 max-w-[720px] text-sm leading-5 text-[#d5d2c9]">
                    {project.description}
                  </p>
                )}
              </div>
              <div className="flex items-center gap-1">
                <div className="relative" data-project-detail-menu-root>
                  <button
                    aria-expanded={openThreadMenuID === projectMenuKey}
                    aria-label="Open project actions"
                    className="grid h-8 w-8 place-items-center rounded-md text-[#d5d2c9] hover:bg-[#2a2a28]"
                    type="button"
                    onClick={() => onToggleThreadMenu(projectMenuKey)}
                  >
                    <Icon name="moreVertical" size="18px" />
                  </button>
                  {openThreadMenuID === projectMenuKey && (
                    <ProjectActionsMenu
                      project={project}
                      archived={project.archivedAt !== undefined}
                      onEdit={onEditProject}
                      onArchive={onArchiveProject}
                      onUnarchive={onUnarchiveProject}
                      onDelete={onDeleteProject}
                    />
                  )}
                </div>
                <button
                  aria-label={project.starred ? "Unstar project" : "Star project"}
                  aria-pressed={project.starred}
                  className="grid h-8 w-8 place-items-center rounded-md text-[#d5d2c9] hover:bg-[#2a2a28]"
                  type="button"
                  onClick={() => onToggleStar(project, !project.starred)}
                >
                  <Icon name={project.starred ? "starFilled" : "star"} size="18px" />
                </button>
              </div>
            </header>
            <div className="mt-10">
              <WindowFileDrop enabled onAttachFiles={handleAttachFiles} onAttachError={handleAttachError} />
              <Composer
                variant="start"
                draft={draft}
                isSending={isSending}
                sendDisabled={sendDisabled || imageUploadPending}
                placeholder="How can I help you today?"
                autoFocus
                onDraftChange={onDraftChange}
                onSend={handleSendRequest}
                onStop={onStop}
                onAttachFiles={handleAttachFiles}
                onAttachError={handleAttachError}
                attachments={attachments}
                onRemoveAttachment={removeAttachment}
              />
            </div>
            {attachNote !== "" && (
              <div className="ui-meta-text mt-2 text-center text-[#858178]">{attachNote}</div>
            )}
            {sendError !== "" && (
              <div className="ui-meta-text mt-3 rounded-md border border-accent px-3 py-2 text-accent">
                {sendError}
              </div>
            )}
            <ul className="mt-6">
              {threads.length === 0 ? (
                <li className="py-10 text-center text-[#807d74]">No chats in this project yet.</li>
              ) : (
                threads.map((thread, index) => {
                  const nextThread = threads[index + 1];
                  const nextActive =
                    nextThread !== undefined &&
                    (hoveredThreadID === nextThread.id || openThreadMenuID === nextThread.id);
                  return (
                    <ChatRow
                      key={thread.id}
                      thread={thread}
                      selectMode={false}
                      selected={false}
                      menuOpen={openThreadMenuID === thread.id}
                      hovered={hoveredThreadID === thread.id}
                      hideDivider={nextActive}
                      onHoverChange={(hovered) => setHoveredThreadID(hovered ? thread.id : null)}
                      onOpen={() => onOpenThread(thread.id)}
                      onToggleSelected={() => undefined}
                      onToggleMenu={() => onToggleThreadMenu(thread.id)}
                      onCloseMenu={onCloseThreadMenu}
                      onSelectFromMenu={() => undefined}
                      onRename={onRenameThread}
                      onDelete={onDeleteThread}
                      onRemoveFromProject={onRemoveFromProject}
                      onStarChange={onStarThread}
                    />
                  );
                })
              )}
            </ul>
          </div>
          <aside className="flex w-full flex-col gap-4 lg:w-[320px] lg:shrink-0">
            <ProjectKnowledgePanel projectId={project.id} />
            <ProjectMemoryPanel projectId={project.id} />
          </aside>
        </div>
      </div>
    </div>
  );
}
