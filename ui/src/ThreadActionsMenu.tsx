import type { Thread } from "./api";
import { Icon } from "./chat/Icon";

// Standard menu entry: inset with rounded corners so the hover highlight floats
// inside the menu (matching menuDeleteItemClass). Geometry only — each entry adds
// its own text color and hover fill (hover:bg-[#3f3f3a], or enabled:hover:... when
// the entry can be disabled).
export const menuItemClass =
  "mx-1 flex min-h-[30px] w-[calc(100%-0.5rem)] items-start gap-2.5 rounded-md px-3 py-1 text-left transition-colors";
export const menuIconClass = "grid h-[21px] w-[21px] shrink-0 place-items-center";
// Destructive menu entry: muted red by default, solid red highlight on hover
// (inset with rounded corners, white text/icon). Shared by every Delete menu.
export const menuDeleteItemClass =
  "mx-1 flex min-h-[30px] w-[calc(100%-0.5rem)] items-start gap-2.5 rounded-md px-3 py-1 text-left text-[#d98278] transition-colors hover:bg-[#d03b3c] hover:text-white";

export function ThreadActionsMenu({
  menuKey,
  thread,
  className = "left-[174px]",
  onSelect,
  onDelete,
  onRename,
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
  onAddToProject?(thread: Thread): void;
  onRemoveFromProject?(thread: Thread): void;
  onStarChange(thread: Thread, starred: boolean, menuKey: string): void;
}) {
  const hasProject = thread.projectId !== undefined && thread.projectId !== null;
  return (
    <div
      aria-label="Thread actions"
      className={`ui-sidebar-text absolute z-20 mt-1 w-[168px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] py-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${className}`}
      role="menu"
    >
      {onSelect !== undefined && (
        <>
          <button
            className={`${menuItemClass} text-[#f3f0e8] hover:bg-[#3f3f3a]`}
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
        className={`${menuItemClass} text-[#f3f0e8] hover:bg-[#3f3f3a]`}
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
        className={`${menuItemClass} text-[#f3f0e8] hover:bg-[#3f3f3a]`}
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
          className={`${menuItemClass} text-[#f3f0e8] enabled:hover:bg-[#3f3f3a] disabled:cursor-default disabled:opacity-100`}
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
          className={`${menuItemClass} text-[#f3f0e8] enabled:hover:bg-[#3f3f3a] disabled:cursor-default disabled:opacity-100`}
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
      <button
        className={menuDeleteItemClass}
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
  return (
    <span className={`${menuIconClass} text-[19px] leading-none`} aria-hidden="true">
      <Icon name="trash" size="19px" />
    </span>
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
  return (
    <span className={`${menuIconClass} text-[19px] leading-none`} aria-hidden="true">
      <Icon name="project" size="19px" />
    </span>
  );
}
