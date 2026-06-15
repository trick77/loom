import type { Thread } from "./api";
import { Icon } from "./chat/Icon";

export const menuItemClass = "flex min-h-[30px] w-full items-start gap-2.5 px-3 py-1 text-left";
export const menuIconClass = "grid h-5 w-[21px] shrink-0 place-items-center";

export function ThreadActionsMenu({
  menuKey,
  thread,
  className = "left-[174px]",
  onSelect,
  onDelete,
  onRename,
  onArchive,
  onAddToProject,
  onRemoveFromProject,
  onStarChange,
}: {
  menuKey: string;
  thread: Thread;
  className?: string;
  onSelect?(thread: Thread): void;
  onDelete(thread: Thread): void;
  onRename(thread: Thread): void;
  onArchive?(thread: Thread): void;
  onAddToProject?(thread: Thread): void;
  onRemoveFromProject?(thread: Thread): void;
  onStarChange(thread: Thread, starred: boolean, menuKey: string): void;
}) {
  const hasProject = thread.projectId !== undefined && thread.projectId !== null;
  return (
    <div
      aria-label="Chat actions"
      className={`ui-sidebar-text absolute z-20 mt-1 w-[168px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${className}`}
      role="menu"
    >
      {onSelect !== undefined && (
        <>
          <button
            className={`${menuItemClass} text-[#f3f0e8]`}
            role="menuitem"
            type="button"
            onClick={() => onSelect(thread)}
          >
            <CheckMenuIcon />
            Select
          </button>
          <MenuSeparator />
        </>
      )}
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => onStarChange(thread, !thread.starred, menuKey)}
      >
        <span className={`${menuIconClass} text-[19px] leading-none`} aria-hidden="true">
          <Icon name={thread.starred ? "starOff" : "star"} size="19px" />
        </span>
        {thread.starred ? "Unstar" : "Star"}
      </button>
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => onRename(thread)}
      >
        <span className={`${menuIconClass} text-[19px] leading-none`} aria-hidden="true">
          <Icon name="edit" size="19px" />
        </span>
        Rename
      </button>
      {hasProject ? (
        <button
          className={`${menuItemClass} text-[#f3f0e8] disabled:cursor-default disabled:opacity-100`}
          disabled={onRemoveFromProject === undefined}
          role="menuitem"
          type="button"
          onClick={() => onRemoveFromProject?.(thread)}
        >
          <ProjectMenuIcon />
          Remove from project
        </button>
      ) : (
        <button
          className={`${menuItemClass} text-[#f3f0e8] disabled:cursor-default disabled:opacity-100`}
          disabled={onAddToProject === undefined}
          role="menuitem"
          type="button"
          onClick={() => onAddToProject?.(thread)}
        >
          <ProjectMenuIcon />
          Add to project
        </button>
      )}
      <MenuSeparator />
      {onArchive !== undefined && (
        <button
          className={`${menuItemClass} text-[#f3f0e8]`}
          role="menuitem"
          type="button"
          onClick={() => onArchive(thread)}
        >
          <ArchiveMenuIcon />
          Archive
        </button>
      )}
      <button
        className={`${menuItemClass} text-[#d98278]`}
        role="menuitem"
        type="button"
        onClick={() => onDelete(thread)}
      >
        <TrashMenuIcon />
        Delete
      </button>
    </div>
  );
}

export function TrashMenuIcon() {
  return <Icon name="trash" size="19px" className={menuIconClass} />;
}

function ArchiveMenuIcon() {
  return (
    <svg className={menuIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path d="M5 8h14v11H5V8Z" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
      <path d="M4 5h16v3H4V5Z" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
      <path d="M9 12h6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}

function MenuSeparator() {
  return <div className="mx-[14px] my-[5px] h-px bg-[#4a4741]" role="separator" />;
}

function CheckMenuIcon() {
  return (
    <svg className={menuIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path d="M5 12.5l4 4 10-10" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function ProjectMenuIcon() {
  return <Icon name="project" size="19px" className={menuIconClass} />;
}
