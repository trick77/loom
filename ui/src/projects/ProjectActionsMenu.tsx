import type { Project } from "../api";
import { menuIconClass, menuItemClass, TrashMenuIcon } from "../ThreadActionsMenu";

export function ProjectActionsMenu({
  project,
  className = "right-0 top-full",
  onEdit,
  onArchive,
  onDelete,
}: {
  project: Project;
  className?: string;
  onEdit(project: Project): void;
  onArchive(project: Project): void;
  onDelete(project: Project): void;
}) {
  return (
    <div
      aria-label="Project actions"
      className={`ui-sidebar-text absolute z-20 mt-1 w-[168px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${className}`}
      role="menu"
    >
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => onEdit(project)}
      >
        <EditIcon />
        Edit details
      </button>
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => onArchive(project)}
      >
        <ArchiveIcon />
        Archive
      </button>
      <div className="mx-[14px] my-[5px] h-px bg-[#4a4741]" role="separator" />
      <button
        className={`${menuItemClass} text-[#d98278]`}
        role="menuitem"
        type="button"
        onClick={() => onDelete(project)}
      >
        <TrashMenuIcon />
        Delete
      </button>
    </div>
  );
}

function EditIcon() {
  return (
    <svg className={menuIconClass} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M5 19h4l10-10-4-4L5 15v4Z" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
      <path d="m13.5 6.5 4 4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  );
}

function ArchiveIcon() {
  return (
    <svg className={menuIconClass} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M5 8h14v11H5V8Z" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
      <path d="M4 5h16v3H4V5Z" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
      <path d="M9 12h6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  );
}
