import { useEffect, useRef } from "react";

import type { Thread } from "../api";
import { Icon } from "../chat/Icon";
import { ThreadActionsMenu } from "../ThreadActionsMenu";
import { formatTimeAgo } from "../timeago";

export function ChatRow({
  thread,
  selectMode,
  selected,
  menuOpen,
  hovered,
  onHoverChange,
  onOpen,
  onToggleSelected,
  onToggleMenu,
  onCloseMenu,
  onSelectFromMenu,
  onRename,
  onDelete,
  onArchive,
  onAddToProject,
  onRemoveFromProject,
  onStarChange,
}: {
  thread: Thread;
  selectMode: boolean;
  selected: boolean;
  menuOpen: boolean;
  hovered: boolean;
  onHoverChange(hovered: boolean): void;
  onOpen(): void;
  onToggleSelected(): void;
  onToggleMenu(): void;
  onCloseMenu(): void;
  onSelectFromMenu(): void;
  onRename(thread: Thread): void;
  onDelete(thread: Thread): void;
  onArchive?(thread: Thread): void;
  onAddToProject?(thread: Thread): void;
  onRemoveFromProject?(thread: Thread): void;
  onStarChange(thread: Thread, starred: boolean, menuKey: string): void;
}) {
  const rowRef = useRef<HTMLLIElement | null>(null);

  useEffect(() => {
    if (!menuOpen) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Node) || rowRef.current?.contains(target)) return;
      onCloseMenu();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [menuOpen, onCloseMenu]);

  const timeLabel = formatTimeAgo(thread.lastMessageAt ?? thread.updatedAt);
  const showMenuButton = hovered || menuOpen;
  const activeSurface = hovered || selected || menuOpen;

  return (
    <li
      ref={rowRef}
      className={`relative border-b ${activeSurface ? "border-transparent" : "border-[#343432]"}`}
      onPointerEnter={() => onHoverChange(true)}
      onPointerLeave={() => onHoverChange(false)}
    >
      <div
        className={`group relative flex h-[49px] items-center gap-3 rounded-xl px-3 transition-colors hover:bg-[#2a2a28] ${
          activeSurface ? "bg-[#2a2a28]" : ""
        }`}
      >
        {selectMode && (
          <button
            type="button"
            role="checkbox"
            aria-checked={selected}
            aria-label={selected ? "Deselect chat" : "Select chat"}
            onClick={onToggleSelected}
            className={`grid h-[18px] w-[18px] shrink-0 place-items-center rounded-md border transition-colors ${
              selected ? "border-[#c6613f] bg-[#c6613f] text-white" : "border-[#56554f] text-transparent"
            }`}
          >
            <svg className="h-3 w-3" viewBox="0 0 24 24" fill="none" aria-hidden="true">
              <path d="M5 12.5l4 4 10-10" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        )}
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-3 text-left"
          onClick={() => (selectMode ? onToggleSelected() : onOpen())}
        >
          <span className="truncate text-[15px] text-[#ecece6]">{thread.title}</span>
          <span
            className={`ml-auto shrink-0 text-[13px] text-[#8a887f] group-hover:hidden ${
              activeSurface ? "hidden" : ""
            }`}
            data-chat-row-time
          >
            {timeLabel}
          </span>
        </button>
        {!selectMode && (
          <button
            aria-expanded={menuOpen}
            aria-label="Open chat actions"
            className={`absolute right-3 grid h-7 w-7 place-items-center rounded-md text-[#d8d4ca] transition-colors hover:bg-[#2a2a28] hover:text-white ${
              showMenuButton ? "" : "invisible"
            }`}
            onClick={(event) => {
              event.stopPropagation();
              onToggleMenu();
            }}
            type="button"
          >
            <Icon name="moreVertical" size="18px" />
          </button>
        )}
      </div>
      {menuOpen && !selectMode && (
        <ThreadActionsMenu
          menuKey={thread.id}
          thread={thread}
          className="right-0"
          onSelect={onSelectFromMenu}
          onDelete={(target) => {
            onCloseMenu();
            onDelete(target);
          }}
          onRename={(target) => {
            onCloseMenu();
            onRename(target);
          }}
          onArchive={
            onArchive === undefined
              ? undefined
              : (target) => {
                  onCloseMenu();
                  onArchive(target);
                }
          }
          onAddToProject={
            onAddToProject === undefined
              ? undefined
              : (target) => {
                  onCloseMenu();
                  onAddToProject(target);
                }
          }
          onRemoveFromProject={
            onRemoveFromProject === undefined
              ? undefined
              : (target) => {
                  onCloseMenu();
                  onRemoveFromProject(target);
                }
          }
          onStarChange={(target, starred, menuKey) => {
            onCloseMenu();
            onStarChange(target, starred, menuKey);
          }}
        />
      )}
    </li>
  );
}
