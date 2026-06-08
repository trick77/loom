import { useCallback, useEffect, useState } from "react";

import { AuthExpiredError, bulkDeleteThreads, listThreads, type Thread } from "../api";
import { BulkDeleteModal } from "./BulkDeleteModal";
import { ChatRow } from "./ChatRow";
import { PillButton } from "./PillButton";

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
  const [reloadToken, setReloadToken] = useState(0);

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
  }, [searchTerm, mutationVersion, reloadToken, onSessionExpired]);

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
      // Optimistically drop the selected rows for instant feedback, then
      // reconcile with the server in case a best-effort delete left some
      // threads behind (partial failure).
      const removed = new Set(ids);
      setThreads((current) => current.filter((thread) => !removed.has(thread.id)));
      setConfirmingDelete(false);
      exitSelectMode();
      setReloadToken((value) => value + 1);
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
              <span className="slopr-control-text text-[#9c9a92]">{selectedCount} selected</span>
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
                className="slopr-control-text rounded-lg px-3 py-1.5 text-[#c7c5bd] transition-colors hover:text-white"
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
            className="slopr-composer-text h-11 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] pl-11 pr-3 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
          />
        </div>

        {loadError !== "" && (
          <div className="slopr-meta-text mt-4 rounded-md border border-accent px-3 py-2 text-accent">
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
