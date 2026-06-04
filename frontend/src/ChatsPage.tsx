import { useCallback, useEffect, useRef, useState } from "react";

import { AuthExpiredError, bulkDeleteThreads, listThreads, type Thread } from "./api";
import { ThreadActionsMenu } from "./ThreadActionsMenu";
import { formatTimeAgo } from "./timeago";

const PAGE_LIMIT = 1000;
const SEARCH_DEBOUNCE_MS = 250;

export function ChatsPage({
  mutationVersion,
  onNewChat,
  onSelectThread,
  onRenameThread,
  onDeleteThread,
  onStarThread,
  onAfterBulkDelete,
  onSessionExpired,
}: {
  mutationVersion: number;
  onNewChat(): void;
  onSelectThread(threadID: string): void;
  onRenameThread(thread: Thread): void;
  onDeleteThread(thread: Thread): void;
  onStarThread(thread: Thread, starred: boolean, menuKey: string): void;
  onAfterBulkDelete(): void;
  onSessionExpired(): void;
}) {
  const [threads, setThreads] = useState<Thread[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const [searchTerm, setSearchTerm] = useState("");
  const [selectMode, setSelectMode] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set());
  const [openMenuID, setOpenMenuID] = useState<string | null>(null);
  const [hoveredID, setHoveredID] = useState<string | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  // Debounce the raw input into the term that actually hits the API.
  useEffect(() => {
    const handle = window.setTimeout(() => setSearchTerm(searchInput.trim()), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(handle);
  }, [searchInput]);

  // Load the full thread list for the current search; refetch when an external
  // mutation (star/rename/single-delete from a row menu) bumps the version.
  useEffect(() => {
    let active = true;
    listThreads({ search: searchTerm, limit: PAGE_LIMIT })
      .then((next) => {
        if (!active) return;
        setThreads(next);
        setLoaded(true);
        setLoadError("");
        // Drop selections that no longer exist in the current result set.
        setSelectedIds((current) => {
          if (current.size === 0) return current;
          const allowed = new Set(next.map((thread) => thread.id));
          const filtered = new Set<string>();
          current.forEach((id) => {
            if (allowed.has(id)) filtered.add(id);
          });
          return filtered;
        });
      })
      .catch((error: unknown) => {
        if (!active) return;
        if (error instanceof AuthExpiredError) {
          onSessionExpired();
          return;
        }
        setLoadError("Chats failed to load.");
      });
    return () => {
      active = false;
    };
  }, [searchTerm, mutationVersion, onSessionExpired]);

  const selectedCount = selectedIds.size;
  const hasSelection = selectedCount > 0;

  const exitSelectMode = useCallback(() => {
    setSelectMode(false);
    setSelectedIds(new Set());
  }, []);

  const toggleSelected = useCallback((threadID: string) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(threadID)) {
        next.delete(threadID);
      } else {
        next.add(threadID);
      }
      return next;
    });
  }, []);

  const toggleSelectAll = useCallback(() => {
    setSelectedIds((current) =>
      current.size === threads.length ? new Set() : new Set(threads.map((thread) => thread.id)),
    );
  }, [threads]);

  const startSelectModeWith = useCallback((thread: Thread) => {
    setOpenMenuID(null);
    setSelectMode(true);
    setSelectedIds(new Set([thread.id]));
  }, []);

  async function handleBulkDelete() {
    if (selectedCount === 0 || isDeleting) return;
    const ids = Array.from(selectedIds);
    setIsDeleting(true);
    try {
      await bulkDeleteThreads(ids);
      const removed = new Set(ids);
      setThreads((current) => current.filter((thread) => !removed.has(thread.id)));
      setConfirmingDelete(false);
      exitSelectMode();
      onAfterBulkDelete();
    } catch (error) {
      if (error instanceof AuthExpiredError) {
        onSessionExpired();
        return;
      }
      setLoadError("Chats failed to delete.");
      setConfirmingDelete(false);
    } finally {
      setIsDeleting(false);
    }
  }

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-[860px] px-6 pb-16 pt-10">
        <header className="flex items-center justify-between">
          <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">Chats</h1>
          {selectMode ? (
            <div className="flex items-center gap-2.5">
              <span className="spark-control-text text-[#9c9a92]">{selectedCount} selected</span>
              <PillButton variant="solid" onClick={toggleSelectAll}>
                Select all
              </PillButton>
              <PillButton variant="muted" enabled={hasSelection} title="Projects are not available yet">
                Move to project
              </PillButton>
              <PillButton
                variant="muted"
                enabled={hasSelection}
                onClick={() => hasSelection && setConfirmingDelete(true)}
              >
                Delete
              </PillButton>
              <button
                type="button"
                className="spark-control-text rounded-lg px-3 py-1.5 text-[#c7c5bd] transition-colors hover:text-white"
                onClick={exitSelectMode}
              >
                Cancel
              </button>
            </div>
          ) : (
            <div className="flex items-center gap-2.5">
              <PillButton variant="solid" onClick={() => setSelectMode(true)}>
                Select chats
              </PillButton>
              <PillButton variant="white" onClick={onNewChat}>
                New chat
              </PillButton>
            </div>
          )}
        </header>

        <div className="relative mt-6">
          <svg
            className="pointer-events-none absolute left-3.5 top-1/2 h-[18px] w-[18px] -translate-y-1/2 text-[#807d74]"
            viewBox="0 0 24 24"
            fill="none"
            aria-hidden="true"
          >
            <circle cx="11" cy="11" r="6" stroke="currentColor" strokeWidth="1.5" />
            <path d="m20 20-3.6-3.6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
          <input
            type="text"
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder="Search chats…"
            aria-label="Search chats"
            className="spark-control-text h-11 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] pl-11 pr-3 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
          />
        </div>

        {loadError !== "" && (
          <div className="spark-meta-text mt-4 rounded-md border border-accent px-3 py-2 text-accent">
            {loadError}
          </div>
        )}

        <ul className="mt-3">
          {threads.length === 0 && loadError === "" ? (
            loaded && (
              <li className="py-10 text-center text-[#807d74]">
                {searchTerm === "" ? "No chats yet." : "No chats match your search."}
              </li>
            )
          ) : (
            threads.map((thread) => (
              <ChatRow
                key={thread.id}
                thread={thread}
                selectMode={selectMode}
                selected={selectedIds.has(thread.id)}
                menuOpen={openMenuID === thread.id}
                hovered={hoveredID === thread.id}
                onHoverChange={(hovered) => setHoveredID(hovered ? thread.id : null)}
                onOpen={() => onSelectThread(thread.id)}
                onToggleSelected={() => toggleSelected(thread.id)}
                onToggleMenu={() =>
                  setOpenMenuID((current) => (current === thread.id ? null : thread.id))
                }
                onCloseMenu={() => setOpenMenuID(null)}
                onSelectFromMenu={() => startSelectModeWith(thread)}
                onRename={onRenameThread}
                onDelete={onDeleteThread}
                onStarChange={onStarThread}
              />
            ))
          )}
        </ul>
      </div>

      {confirmingDelete && (
        <BulkDeleteModal
          count={selectedCount}
          disabled={isDeleting}
          onCancel={() => setConfirmingDelete(false)}
          onConfirm={() => void handleBulkDelete()}
        />
      )}
    </div>
  );
}

function PillButton({
  children,
  variant,
  enabled = true,
  onClick,
  title,
}: {
  children: React.ReactNode;
  variant: "solid" | "white" | "muted";
  enabled?: boolean;
  onClick?(): void;
  title?: string;
}) {
  let className = "spark-control-text rounded-lg px-3 py-1.5 font-medium transition-colors ";
  if (variant === "solid") {
    className += "bg-[#343433] text-[#f5f3ee] hover:bg-[#3d3d3b]";
  } else if (variant === "white") {
    className += "bg-white text-[#1d1d1c] hover:bg-[#ece9e2]";
  } else {
    // muted: keeps its background in both states; only the label brightness changes.
    className += `bg-[#282827] ${enabled ? "text-[#faf9f5]" : "text-[#8c8a82]"}`;
  }
  return (
    <button
      type="button"
      className={className}
      onClick={onClick}
      disabled={!enabled}
      aria-disabled={!enabled}
      title={title}
    >
      {children}
    </button>
  );
}

function ChatRow({
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

  return (
    <li
      ref={rowRef}
      className="relative border-b border-[#343432]"
      // JS-driven hover so the reveal toggle works reliably in Safari.
      onPointerEnter={() => onHoverChange(true)}
      onPointerLeave={() => onHoverChange(false)}
    >
      <div className="flex h-[49px] items-center gap-3">
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
          <span className="shrink-0 text-[13px] text-[#8a887f]">{timeLabel}</span>
        </button>
        {!selectMode && (
          <button
            aria-expanded={menuOpen}
            aria-label="Open chat actions"
            className={`grid h-7 w-7 shrink-0 place-items-center rounded-md text-[#d8d4ca] transition-colors hover:bg-[#2a2a28] hover:text-white ${
              showMenuButton ? "" : "invisible"
            }`}
            onClick={(event) => {
              event.stopPropagation();
              onToggleMenu();
            }}
            type="button"
          >
            <span aria-hidden="true" className="flex h-[10px] flex-col items-center justify-between">
              <span className="h-0.5 w-0.5 rounded-full bg-current" />
              <span className="h-0.5 w-0.5 rounded-full bg-current" />
              <span className="h-0.5 w-0.5 rounded-full bg-current" />
            </span>
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
          onStarChange={(target, starred, menuKey) => {
            onCloseMenu();
            onStarChange(target, starred, menuKey);
          }}
        />
      )}
    </li>
  );
}

function BulkDeleteModal({
  count,
  disabled,
  onCancel,
  onConfirm,
}: {
  count: number;
  disabled: boolean;
  onCancel(): void;
  onConfirm(): void;
}) {
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onCancel();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onCancel]);

  const label = count === 1 ? "this chat" : `these ${count} chats`;
  return (
    <div
      className="fixed inset-0 z-40 grid place-items-center bg-[rgba(10,10,9,0.62)] pr-4 pl-[378px]"
      onClick={(event) => {
        if (event.target === event.currentTarget) onCancel();
      }}
    >
      <div
        aria-modal="true"
        role="dialog"
        className="w-full max-w-[390px] rounded-xl border border-[#4b4a46] bg-[#2a2a28] p-[18px] shadow-[0_28px_70px_rgba(0,0,0,0.55)]"
      >
        <h2 className="font-sans text-[22px] font-semibold leading-7 text-[#f3f0e8]">
          {count === 1 ? "Delete chat" : `Delete ${count} chats`}
        </h2>
        <div className="mt-3 text-[13px] leading-5 text-[#d8d4ca]">
          Are you sure you want to delete {label}? This cannot be undone.
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <button
            autoFocus
            className="h-8 rounded-md px-3 text-[#c7c5bd] hover:bg-[#363632]"
            onClick={onCancel}
            type="button"
          >
            Cancel
          </button>
          <button
            className="h-8 rounded-md bg-[#b85c52] px-3.5 font-medium text-[#fffaf2] disabled:opacity-50"
            disabled={disabled}
            onClick={onConfirm}
            type="button"
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}
