import { type ReactNode, useEffect, useRef } from "react";

import type { Project, Thread } from "../api";
import { menuDeleteItemClass, menuIconClass, menuItemClass, ThreadActionsMenu, TrashMenuIcon } from "../ThreadActionsMenu";
import { ArchiveIcon } from "../projects/ProjectActionsMenu";
import { Icon } from "./Icon";
import type { SidebarIconName } from "./types";

export function SidebarPrimaryItem({
  icon,
  label,
  collapsed = false,
  active = false,
  onClick,
}: {
  icon: SidebarIconName;
  label: string;
  collapsed?: boolean;
  active?: boolean;
  onClick?(): void;
}) {
  const className = `flex h-7 w-full items-center rounded-md px-1.5 text-left text-[#c7c5bd] ${
    collapsed ? "justify-center" : "gap-2.5"
  } ${active ? "bg-[#111110]" : ""} ${onClick !== undefined ? "transition-colors hover:bg-[#2a2a28]" : ""}`;
  const content = (
    <>
      <SidebarIcon name={icon} />
      {!collapsed && <span className="truncate">{label}</span>}
    </>
  );
  if (onClick === undefined) {
    return <div className={className}>{content}</div>;
  }
  return (
    <button type="button" className={className} onClick={onClick} aria-label={label}>
      {content}
    </button>
  );
}

function SidebarIcon({ name }: { name: SidebarIconName }) {
  const className = "h-[21px] w-[21px] shrink-0 text-[#f0eee7]";
  if (name === "threads") {
    return <Icon name="messages" size="21px" className={className} />;
  }
  if (name === "projects") {
    return <Icon name="archive" size="21px" className={className} />;
  }
  if (name === "artifacts") {
    return <Icon name="artifact" size="21px" className={className} />;
  }
  if (name === "memory") {
    return <Icon name="memory" size="21px" className={className} />;
  }
  return null;
}

export function SidebarSection({
  title,
  threads,
  activeThreadID,
  openThreadMenuID,
  onSelect,
  onDelete,
  onRename,
  onAddToProject,
  onStarChange,
  onToggleMenu,
  onCloseMenu,
  leading,
}: {
  title: string;
  threads: Thread[];
  activeThreadID: string | null;
  openThreadMenuID: string | null;
  onSelect(threadID: string): void;
  onDelete(thread: Thread): void;
  onRename(thread: Thread): void;
  onAddToProject?(thread: Thread): void;
  onStarChange(thread: Thread, starred: boolean, menuKey: string): void;
  onToggleMenu(menuKey: string): void;
  onCloseMenu(): void;
  leading?: ReactNode;
}) {
  return (
    <section className="mt-5">
      <div className="ui-meta-text mb-2 px-1.5 text-[#97958c]">{title}</div>
      <div className="space-y-1.5">
        {leading}
        {threads.map((thread) => (
          <SidebarThreadItem
            key={thread.id}
            menuKey={`${title}:${thread.id}`}
            thread={thread}
            active={activeThreadID === thread.id}
            menuOpen={openThreadMenuID === `${title}:${thread.id}`}
            onSelect={onSelect}
            onDelete={onDelete}
            onRename={onRename}
            onAddToProject={onAddToProject}
            onStarChange={onStarChange}
            onToggleMenu={onToggleMenu}
            onCloseMenu={onCloseMenu}
          />
        ))}
      </div>
    </section>
  );
}

function SidebarThreadItem({
  menuKey,
  thread,
  active,
  menuOpen,
  onSelect,
  onDelete,
  onRename,
  onAddToProject,
  onStarChange,
  onToggleMenu,
  onCloseMenu,
}: {
  menuKey: string;
  thread: Thread;
  active: boolean;
  menuOpen: boolean;
  onSelect(threadID: string): void;
  onDelete(thread: Thread): void;
  onRename(thread: Thread): void;
  onAddToProject?(thread: Thread): void;
  onStarChange(thread: Thread, starred: boolean, menuKey: string): void;
  onToggleMenu(menuKey: string): void;
  onCloseMenu(): void;
}) {
  const itemRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!menuOpen) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Node) || itemRef.current?.contains(target)) return;
      onCloseMenu();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [menuOpen, onCloseMenu]);

  return (
    <div ref={itemRef} className="relative">
      <div
        className={`group flex h-7 w-full items-center rounded-md py-0 pl-1.5 pr-1 text-left transition-colors ${
          active ? "bg-[#10100f] text-white" : "hover:bg-[#2a2a28]"
        }`}
      >
        <button className="relative min-w-0 flex-1 overflow-hidden text-left" onClick={() => onSelect(thread.id)} type="button">
          <span className={`block whitespace-nowrap ${active ? "pr-7" : "truncate"}`}>{thread.title}</span>
          {active && (
            <span className="pointer-events-none absolute inset-y-0 right-0 w-9 bg-gradient-to-r from-transparent to-[#10100f]" aria-hidden="true" />
          )}
        </button>
        <button
          aria-expanded={menuOpen}
          aria-label="Open thread actions"
          // Keep inactive rows visually quiet while preserving keyboard access
          // to the thread actions.
          className={`grid h-6 w-6 shrink-0 place-items-center rounded-md text-[#d8d4ca] transition-colors hover:bg-[#2a2a28] hover:text-white ${
            active || menuOpen ? "" : "invisible group-hover:visible group-focus-within:visible [@media(hover:none)]:visible"
          }`}
          onClick={(event) => {
            event.stopPropagation();
            onToggleMenu(menuKey);
          }}
          type="button"
        >
          <Icon name="moreVertical" size="18px" />
        </button>
      </div>
      {menuOpen && (
        <ThreadActionsMenu
          menuKey={menuKey}
          thread={thread}
          className="right-4 left-auto md:left-[160px] md:right-auto"
          onDelete={onDelete}
          onRename={onRename}
          onAddToProject={onAddToProject}
          onStarChange={onStarChange}
        />
      )}
    </div>
  );
}

export function SidebarProjectItem({
  menuKey,
  project,
  active,
  menuOpen,
  onNavigate,
  onStarChange,
  onEdit,
  onArchive,
  onDelete,
  onToggleMenu,
  onCloseMenu,
}: {
  menuKey: string;
  project: Project;
  active: boolean;
  menuOpen: boolean;
  onNavigate(project: Project): void;
  onStarChange(project: Project, starred: boolean, menuKey: string): void;
  onEdit(project: Project): void;
  onArchive(project: Project): void;
  onDelete(project: Project): void;
  onToggleMenu(menuKey: string): void;
  onCloseMenu(): void;
}) {
  const itemRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!menuOpen) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Node) || itemRef.current?.contains(target)) return;
      onCloseMenu();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [menuOpen, onCloseMenu]);

  return (
    <div ref={itemRef} className="relative">
      <div
        className={`group flex h-7 w-full items-center rounded-md py-0 pl-1.5 pr-1 text-left transition-colors ${
          active ? "bg-[#111110] text-white" : "hover:bg-[#2a2a28]"
        }`}
      >
        <button className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden text-left" onClick={() => onNavigate(project)} type="button">
          {project.starred && (
            <span className="grid h-[21px] w-[21px] shrink-0 place-items-center text-[#97958c]" aria-hidden="true">
              <Icon name="archive" size="21px" />
            </span>
          )}
          <span className="min-w-0 flex-1 truncate whitespace-nowrap">{project.name}</span>
        </button>
        <button
          aria-expanded={menuOpen}
          aria-label="Open project actions"
          // Keep inactive rows visually quiet while preserving keyboard access
          // to the project actions.
          className={`grid h-6 w-6 shrink-0 place-items-center rounded-md text-[#d8d4ca] transition-colors hover:bg-[#2a2a28] hover:text-white ${
            active || menuOpen ? "" : "invisible group-hover:visible group-focus-within:visible [@media(hover:none)]:visible"
          }`}
          onClick={(event) => {
            event.stopPropagation();
            onToggleMenu(menuKey);
          }}
          type="button"
        >
          <Icon name="moreVertical" size="18px" />
        </button>
      </div>
      {menuOpen && (
        <ProjectSidebarMenu
          project={project}
          className="right-4 left-auto"
          onStarChange={(target, starred) => onStarChange(target, starred, menuKey)}
          onEdit={onEdit}
          onArchive={onArchive}
          onDelete={onDelete}
        />
      )}
    </div>
  );
}

function ProjectSidebarMenu({
  project,
  className = "right-0 top-full",
  onStarChange,
  onEdit,
  onArchive,
  onDelete,
}: {
  project: Project;
  className?: string;
  onStarChange(project: Project, starred: boolean): void;
  onEdit(project: Project): void;
  onArchive(project: Project): void;
  onDelete(project: Project): void;
}) {
  return (
    <div
      aria-label="Project actions"
      className={`ui-sidebar-text absolute z-20 mt-1 w-[155px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] py-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${className}`}
      role="menu"
    >
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => onStarChange(project, !project.starred)}
      >
        <span className={`${menuIconClass} text-[19px] leading-none`} aria-hidden="true">
          <Icon name={project.starred ? "starOff" : "star"} size="19px" />
        </span>
        {project.starred ? "Unstar" : "Star"}
      </button>
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => onEdit(project)}
      >
        <span className={`${menuIconClass} text-[19px] leading-none`} aria-hidden="true">
          <Icon name="edit" size="19px" />
        </span>
        Edit details
      </button>
      <div className="mx-[14px] my-[5px] h-px bg-[#454540]" role="separator" />
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => onArchive(project)}
      >
        <ArchiveIcon />
        Archive
      </button>
      <button
        className={menuDeleteItemClass}
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
