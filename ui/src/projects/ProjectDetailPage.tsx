import type { Project, Thread } from "../api";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { ThreadActionsMenu } from "../ThreadActionsMenu";
import { ProjectActionsMenu } from "./ProjectActionsMenu";

export function ProjectDetailPage({
  project,
  threads,
  draft,
  sendError,
  isSending,
  openThreadMenuID,
  onBack,
  onDraftChange,
  onSend,
  onStop,
  onOpenThread,
  onRenameThread,
  onDeleteThread,
  onStarThread,
  onArchiveThread: _onArchiveThread,
  onRemoveFromProject: _onRemoveFromProject,
  onToggleThreadMenu,
  onCloseThreadMenu: _onCloseThreadMenu,
  onEditProject,
  onArchiveProject,
  onDeleteProject,
  onOpenSidebar,
}: {
  project: Project;
  threads: Thread[];
  draft: string;
  sendError: string;
  isSending: boolean;
  openThreadMenuID: string | null;
  onBack(): void;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
  onOpenThread(threadID: string): void;
  onRenameThread(thread: Thread): void;
  onDeleteThread(thread: Thread): void;
  onArchiveThread(thread: Thread): void;
  onStarThread(thread: Thread, starred: boolean, menuKey: string): void;
  onRemoveFromProject(thread: Thread): void;
  onToggleThreadMenu(menuKey: string): void;
  onCloseThreadMenu(): void;
  onEditProject(project: Project): void;
  onArchiveProject(project: Project): void;
  onDeleteProject(project: Project): void;
  onOpenSidebar(): void;
}) {
  const projectMenuKey = `Project:${project.id}`;

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-[920px] px-4 pb-16 pt-10 md:px-6">
        <button
          aria-label="All projects"
          className="slopr-control-text flex items-center gap-2 text-[#c7c5bd] hover:text-white"
          type="button"
          onClick={onBack}
        >
          &larr; All projects
        </button>
        <header className="mt-7 flex items-start justify-between gap-4">
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
          <div className="relative">
            <button
              aria-expanded={openThreadMenuID === projectMenuKey}
              aria-label="Open project actions"
              className="grid h-8 w-8 place-items-center rounded-md text-[#d5d2c9] hover:bg-[#2a2a28]"
              type="button"
              onClick={() => onToggleThreadMenu(projectMenuKey)}
            >
              ...
            </button>
            {openThreadMenuID === projectMenuKey && (
              <ProjectActionsMenu
                project={project}
                onEdit={onEditProject}
                onArchive={onArchiveProject}
                onDelete={onDeleteProject}
              />
            )}
          </div>
        </header>
        <form
          className="mt-10 rounded-[14px] bg-[#2f2f2c] p-4"
          onSubmit={(event) => {
            event.preventDefault();
            onSend();
          }}
        >
          <textarea
            className="min-h-[72px] w-full resize-none bg-transparent text-[17px] text-[#f4f0e8] outline-none placeholder:text-[#8f8b82]"
            placeholder="How can I help you today?"
            value={draft}
            onChange={(event) => onDraftChange(event.target.value)}
          />
          <div className="flex justify-between">
            <button className="grid h-9 w-9 place-items-center rounded-md text-xl text-white hover:bg-[#3a3a37]" type="button">
              +
            </button>
            {isSending ? (
              <button className="rounded-md bg-[#3a3a37] px-3 py-2 text-sm text-white" type="button" onClick={onStop}>
                Stop
              </button>
            ) : (
              <button
                className="rounded-md bg-accent px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
                type="submit"
                disabled={draft.trim() === ""}
              >
                Send
              </button>
            )}
          </div>
        </form>
        {sendError !== "" && (
          <div className="slopr-meta-text mt-3 rounded-md border border-accent px-3 py-2 text-accent">
            {sendError}
          </div>
        )}
        <ul className="mt-6 divide-y divide-[#343432]">
          {threads.length === 0 ? (
            <li className="py-10 text-center text-[#807d74]">No chats in this project yet.</li>
          ) : (
            threads.map((thread) => {
              const menuKey = `ProjectThread:${thread.id}`;
              return (
                <li key={thread.id} className="relative flex items-center justify-between py-4">
                  <button
                    className="min-w-0 truncate text-left text-sm font-medium text-[#f4f0e8]"
                    type="button"
                    onClick={() => onOpenThread(thread.id)}
                  >
                    {thread.title}
                  </button>
                  <button
                    aria-expanded={openThreadMenuID === menuKey}
                    aria-label="Open chat actions"
                    className="grid h-8 w-8 place-items-center rounded-md text-[#d5d2c9] hover:bg-[#2a2a28]"
                    type="button"
                    onClick={() => onToggleThreadMenu(menuKey)}
                  >
                    ...
                  </button>
                  {openThreadMenuID === menuKey && (
                    <ThreadActionsMenu
                      menuKey={menuKey}
                      thread={thread}
                      className="right-0 top-10"
                      onDelete={onDeleteThread}
                      onRename={onRenameThread}
                      onStarChange={onStarThread}
                    />
                  )}
                </li>
              );
            })
          )}
        </ul>
      </div>
    </div>
  );
}
