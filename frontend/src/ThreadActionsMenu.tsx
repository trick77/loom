import type { Thread } from "./api";

export function ThreadActionsMenu({
  menuKey,
  thread,
  className = "left-[174px]",
  onSelect,
  onDelete,
  onRename,
  onStarChange,
}: {
  menuKey: string;
  thread: Thread;
  className?: string;
  onSelect?(thread: Thread): void;
  onDelete(thread: Thread): void;
  onRename(thread: Thread): void;
  onStarChange(thread: Thread, starred: boolean, menuKey: string): void;
}) {
  return (
    <div
      aria-label="Chat actions"
      className={`slop-sidebar-text absolute z-20 mt-1 w-[168px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${className}`}
      role="menu"
    >
      {onSelect !== undefined && (
        <>
          <button
            className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8]"
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
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8]"
        role="menuitem"
        type="button"
        onClick={() => onStarChange(thread, !thread.starred, menuKey)}
      >
        <span className="grid h-[21px] w-[21px] shrink-0 place-items-center text-[19px] leading-none" aria-hidden="true">
          {thread.starred ? "★" : "☆"}
        </span>
        {thread.starred ? "Unstar" : "Star"}
      </button>
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8]"
        role="menuitem"
        type="button"
        onClick={() => onRename(thread)}
      >
        <span className="grid h-[21px] w-[21px] shrink-0 place-items-center text-[19px] leading-none" aria-hidden="true">
          ✎
        </span>
        Rename
      </button>
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#f3f0e8] disabled:cursor-default disabled:opacity-100"
        disabled
        role="menuitem"
        type="button"
      >
        <ProjectMenuIcon />
        Add to project
      </button>
      <MenuSeparator />
      <button
        className="flex h-[34px] w-full items-center gap-2.5 px-3 text-left text-[#d98278]"
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

function MenuSeparator() {
  return <div className="mx-[14px] my-[5px] h-px bg-[#4a4741]" role="separator" />;
}

function CheckMenuIcon() {
  return (
    <svg className="h-[21px] w-[21px] shrink-0" viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path d="M5 12.5l4 4 10-10" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function ProjectMenuIcon() {
  return (
    <svg className="h-[21px] w-[21px] shrink-0" viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path
        d="M4.5 8.5h5l1.6 2h8.4v7.2c0 1.2-.7 1.8-2 1.8h-11c-1.3 0-2-.6-2-1.8V8.5Z"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path
        d="M4.5 8.5V6.8c0-1.1.7-1.7 1.9-1.7h3.1l1.6 2h6.5c1.2 0 1.9.6 1.9 1.7v1.7"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
    </svg>
  );
}

export function TrashMenuIcon() {
  return (
    <svg className="h-[21px] w-[21px] shrink-0" viewBox="0 0 24 24" aria-hidden="true" fill="none">
      <path
        d="M8 7.5V6.2c0-.9.6-1.4 1.5-1.4h5c.9 0 1.5.5 1.5 1.4v1.3"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
      />
      <path d="M5.5 7.5h13" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path
        d="M7.2 9.5l.6 8.1c.1 1 .8 1.6 1.8 1.6h4.8c1 0 1.7-.6 1.8-1.6l.6-8.1"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <path d="M10.4 11.3v5M13.6 11.3v5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  );
}
