import { useCallback, useEffect, useState } from "react";

import {
  AuthExpiredError,
  bulkDeleteThreads,
  listThreadIds,
  listThreads,
  type Thread,
} from "../api";
import { useInfiniteList } from "../useInfiniteList";
import { useThreadSearch } from "../search/useThreadSearch";
import { BulkDeleteModal } from "./BulkDeleteModal";
import { ThreadRow } from "./ThreadRow";
import { Icon } from "../chat/Icon";
import { PillButton } from "./PillButton";
import { SidebarOpenButton } from "../SidebarOpenButton";

const PAGE_SIZE = 50;
const SEARCH_DEBOUNCE_MS = 250;
// Unlike the sidebar modal (capped at 20), the Threads page shows search matches
// uncapped-in-practice so "Select all" can't silently miss matches beyond a small
// cap. Bounded only to keep one render cheap; matches the backend search ceiling.
const THREADS_SEARCH_LIMIT = 200;

export function ThreadsPage({
  mutationVersion,
  projectsAvailable = false,
  onOpenSidebar,
  onNewThread,
  onSelectThread,
  onRenameThread,
  onDeleteThread,
  onStarThread,
  onAddThreadToProject,
  onMoveSelectedToProject,
  onAfterBulkDelete,
  onSessionExpired,
}: {
  mutationVersion: number;
  projectsAvailable?: boolean;
  onOpenSidebar(): void;
  onNewThread(): void;
  onSelectThread(threadID: string): void;
  onRenameThread(thread: Thread): void;
  onDeleteThread(thread: Thread): void;
  onStarThread(thread: Thread, starred: boolean, menuKey: string): void;
  onAddThreadToProject?(thread: Thread): void;
  onMoveSelectedToProject?(threads: Thread[]): void;
  onAfterBulkDelete(): void;
  onSessionExpired(): void;
}) {
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

  // Infinite scroll: load page one (and reset to it) whenever the search changes
  // or an external mutation (star/rename/single-delete from a row menu) or a bulk
  // delete bumps a token; further pages append as the sentinel scrolls into view.
  const fetchPage = useCallback(
    (cursor: string | null) => listThreads({ search: searchTerm, limit: PAGE_SIZE, cursor }),
    [searchTerm],
  );
  const {
    items: threads,
    setItems: setThreads,
    loaded,
    loadingMore,
    hasMore,
    error,
    sentinelRef,
  } = useInfiniteList(fetchPage, [searchTerm, mutationVersion, reloadToken]);

  // While a search is active, mirror the sidebar modal: a merged list of title +
  // full-text matches with snippets (no infinite scroll). With an empty box, fall
  // back to the full paginated management list above. `search` runs an extra
  // recents fetch when not searching, which is ignored. A mutation (single-row
  // star/rename/delete via `mutationVersion`, or a bulk delete via `reloadToken`)
  // refetches the search so deleted/changed rows drop out.
  const showingSearch = searchTerm !== "";
  const search = useThreadSearch(searchTerm, {
    limit: THREADS_SEARCH_LIMIT,
    reloadToken: mutationVersion + reloadToken,
  });
  const rows: { thread: Thread; snippet?: string }[] = showingSearch
    ? search.results
    : threads.map((thread) => ({ thread }));
  const listLoaded = showingSearch ? !search.titleLoading : loaded;
  // The set the bulk actions operate on: the merged search results while
  // searching (so content-only matches are included), else the paginated list.
  const visibleThreads = showingSearch ? rows.map((row) => row.thread) : threads;

  useEffect(() => {
    if (error instanceof AuthExpiredError) {
      onSessionExpired();
      return;
    }
    setLoadError(error !== null ? "Threads failed to load." : "");
  }, [error, onSessionExpired]);

  // A new search changes what "all" means, so clear any selection it carried.
  useEffect(() => {
    setSelectedIds(new Set());
  }, [searchTerm]);

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

  // "Select all" acts on every thread the user can currently see. While
  // searching that's the merged title + full-text result set (content-only
  // matches included), which lives client-side — so select their ids directly
  // rather than refetching a title-only id set from the server. With an empty
  // box it's the full management list, whose later pages aren't loaded yet, so
  // fetch the complete id set. Clicking again, when everything is already
  // selected, clears the selection.
  const toggleSelectAll = useCallback(() => {
    const applyToggle = (ids: string[]) =>
      setSelectedIds((current) => {
        const allSelected = ids.length > 0 && ids.every((id) => current.has(id));
        return allSelected ? new Set() : new Set(ids);
      });
    if (showingSearch) {
      applyToggle(visibleThreads.map((thread) => thread.id));
      return;
    }
    void (async () => {
      try {
        applyToggle(await listThreadIds({ search: searchTerm }));
      } catch (error) {
        if (error instanceof AuthExpiredError) {
          onSessionExpired();
          return;
        }
        setLoadError("Threads failed to load.");
      }
    })();
  }, [showingSearch, visibleThreads, searchTerm, onSessionExpired]);

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
      setLoadError("Threads failed to delete.");
      setConfirmingDelete(false);
    } finally {
      setIsDeleting(false);
    }
  }

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-[802px] px-4 pb-16 pt-10 md:px-6">
        <header className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <SidebarOpenButton onClick={onOpenSidebar} />
            <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">Threads</h1>
          </div>
          {selectMode ? (
            <div className="flex flex-wrap items-center gap-2.5">
              <span className="ui-control-text text-[#9c9a92]">{selectedCount} selected</span>
              <PillButton variant="solid" onClick={toggleSelectAll}>
                Select all
              </PillButton>
              <PillButton
                variant="muted"
                enabled={hasSelection && projectsAvailable}
                title={projectsAvailable ? undefined : "Create a project before moving threads"}
                onClick={() => {
                  if (!hasSelection || !projectsAvailable || onMoveSelectedToProject === undefined) return;
                  onMoveSelectedToProject(visibleThreads.filter((thread) => selectedIds.has(thread.id)));
                }}
              >
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
                className="ui-control-text rounded-lg px-3 py-1.5 text-[#c7c5bd] transition-colors hover:text-white"
                onClick={exitSelectMode}
              >
                Cancel
              </button>
            </div>
          ) : (
            <div className="flex flex-wrap items-center gap-2.5">
              <PillButton variant="solid" onClick={() => setSelectMode(true)}>
                Select threads
              </PillButton>
              <PillButton variant="white" onClick={onNewThread}>
                New thread
              </PillButton>
            </div>
          )}
        </header>

        <div className="relative mt-6">
          <Icon
            name="search"
            size="18px"
            className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-[#807d74]"
          />
          <input
            type="text"
            autoFocus
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder="Search threads…"
            aria-label="Search threads"
            className="ui-composer-text h-11 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] pl-11 pr-3 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
          />
        </div>

        {loadError !== "" && (
          <div className="ui-meta-text mt-4 rounded-md border border-accent px-3 py-2 text-accent">
            {loadError}
          </div>
        )}

        <ul className="mt-3">
          {rows.length === 0 && loadError === "" ? (
            listLoaded && (
              <li className="py-10 text-center text-[#807d74]">
                {searchTerm === "" ? "No threads yet." : "No threads match your search."}
              </li>
            )
          ) : (
            rows.map(({ thread, snippet }, index) => {
              const nextThread = rows[index + 1]?.thread;
              const nextActive =
                nextThread !== undefined &&
                (hoveredID === nextThread.id || openMenuID === nextThread.id || selectedIds.has(nextThread.id));
              return (
                <ThreadRow
                  key={thread.id}
                  thread={thread}
                  snippet={snippet}
                  searchQuery={showingSearch ? searchTerm : undefined}
                  selectMode={selectMode}
                  selected={selectedIds.has(thread.id)}
                  menuOpen={openMenuID === thread.id}
                  hovered={hoveredID === thread.id}
                  hideDivider={nextActive}
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
                  onAddToProject={projectsAvailable ? onAddThreadToProject : undefined}
                  onStarChange={onStarThread}
                />
              );
            })
          )}
        </ul>
        {/* Sentinel observed for infinite scroll; loads the next page when in view.
            Disabled during a search, which shows a capped, non-paginated list. */}
        {!showingSearch && <div ref={sentinelRef} aria-hidden="true" className="h-px" />}
        {!showingSearch && loadingMore && hasMore && (
          <div className="ui-meta-text mt-3 px-1.5 text-[#8a887f]">Loading more…</div>
        )}
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
