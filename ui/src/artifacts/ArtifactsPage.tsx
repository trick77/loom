import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";

import {
  AuthExpiredError,
  deleteArtifact,
  downloadArtifact,
  getArtifactSummary,
  listArtifacts,
  renameArtifact,
  type Artifact,
  type ArtifactListType,
  type ArtifactSummary,
} from "../api";
import { Icon } from "../chat/Icon";
import { downloadBlob } from "../chat/download";
import { isImageAttachment } from "../chat/useDocumentAttachments";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { useInfiniteList } from "../useInfiniteList";
import {
  DeleteArtifactModal,
  RenameArtifactModal,
} from "./ArtifactActionModals";
import type { ArtifactItemMenuProps } from "./ArtifactActionsMenu";
import { ArtifactRow, chatOf } from "./ArtifactRow";
import { ArtifactTile } from "./ArtifactTile";
import {
  type ArtifactGroup,
  type ArtifactPeriod,
  type GroupMode,
  groupArtifacts,
  newestOlderYear,
  periodBounds,
  periodKey,
  recentWindowStart,
  sameLocalDay,
} from "./artifactGroups";
import { formatMonth, PeriodBar } from "./PeriodBar";

// The recent window comes in one request, so no group shows half-loaded,
// unless it holds more than RECENT_LIMIT; a year, a month or a search loads
// page-wise as the list scrolls.
const RECENT_LIMIT = 500;
const PAGE_SIZE = 200;
const SEARCH_DEBOUNCE_MS = 250;
// How often a foreground page checks whether the local day changed.
const DAY_CHECK_MS = 60_000;

export function ArtifactsPage({
  onOpenSidebar,
  onSessionExpired,
  onUseInThread,
  onOpenThread,
}: {
  onOpenSidebar(): void;
  onSessionExpired(): void;
  // Start a new chat with this artifact pre-attached (no re-upload). Only wired
  // for artifacts that can be referenced as an image input.
  onUseInThread(artifact: Artifact): void;
  // Open the chat an artifact came from.
  onOpenThread(threadId: string): void;
}) {
  const { t, i18n } = useTranslation();
  // The page's clock: the window start is part of the fetch, and a fresh one
  // per render would reload the list forever. It moves only to another local
  // day, seen by a tick while the tab is in the foreground (open across
  // midnight) or when it is shown again (a hidden tab left overnight).
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    function checkDay() {
      if (document.visibilityState !== "visible") return;
      // Read now: React may run the updater later.
      const seenAt = new Date();
      setNow((prev) => (sameLocalDay(prev, seenAt) ? prev : seenAt));
    }
    document.addEventListener("visibilitychange", checkDay);
    const tick = window.setInterval(checkDay, DAY_CHECK_MS);
    return () => {
      document.removeEventListener("visibilitychange", checkDay);
      window.clearInterval(tick);
    };
  }, []);
  const recentSince = useMemo(
    () => recentWindowStart(now).toISOString(),
    [now],
  );
  const [searchInput, setSearchInput] = useState("");
  // The term searched for. Typing sets it after a pause; an emptied box ends
  // the search at once and drops the pending term, so neither a picked period
  // nor the next keystroke re-sends the old search.
  const [searchTerm, setSearchTerm] = useState("");
  const searchTimer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(searchTimer.current), []);
  function changeSearch(value: string) {
    setSearchInput(value);
    window.clearTimeout(searchTimer.current);
    const term = value.trim();
    if (term === "") {
      setSearchTerm("");
      return;
    }
    searchTimer.current = window.setTimeout(
      () => setSearchTerm(term),
      SEARCH_DEBOUNCE_MS,
    );
  }
  const searching = searchTerm !== "";
  const [type, setType] = useState<ArtifactListType>("all");
  const [period, setPeriod] = useState<ArtifactPeriod>({ kind: "recent" });
  const currentPeriodKey = periodKey(period);
  const [hoveredArtifactID, setHoveredArtifactID] = useState<string | null>(
    null,
  );
  const [openMenuID, setOpenMenuID] = useState<string | null>(null);
  const [renameTarget, setRenameTarget] = useState<Artifact | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Artifact | null>(null);
  const [actionError, setActionError] = useState("");
  const [actionPending, setActionPending] = useState(false);
  const [downloadError, setDownloadError] = useState("");

  // The parent may hand a fresh callback each render; the summary effect must
  // not refetch for that.
  const onSessionExpiredRef = useRef(onSessionExpired);
  useLayoutEffect(() => {
    onSessionExpiredRef.current = onSessionExpired;
  });

  async function handleDownload(artifact: Artifact) {
    setDownloadError("");
    try {
      downloadBlob(
        await downloadArtifact(artifact.downloadUrl),
        artifact.displayFilename,
      );
    } catch (err) {
      if (err instanceof AuthExpiredError) onSessionExpired();
      else setDownloadError(t("artifactCard.downloadFailed"));
    }
  }

  // The server owns filtering and ordering (newest first); the client renders
  // pages in the order they arrive so cursor boundaries stay aligned. A search
  // spans all time, whatever the period.
  const fetchPage = useCallback(
    (cursor: string | null) => {
      if (searching)
        return listArtifacts({
          type,
          search: searchTerm,
          limit: PAGE_SIZE,
          cursor,
        });
      const bounds = periodBounds(period);
      if (bounds === null)
        return listArtifacts({
          type,
          since: recentSince,
          limit: RECENT_LIMIT,
          cursor,
        });
      return listArtifacts({
        type,
        since: bounds.since.toISOString(),
        until: bounds.until.toISOString(),
        limit: PAGE_SIZE,
        cursor,
      });
    },
    [searching, searchTerm, type, period, recentSince],
  );
  const {
    items: artifacts,
    setItems,
    loaded,
    loadingMore,
    hasMore,
    paged,
    error,
    sentinelRef,
  } = useInfiniteList(fetchPage, [
    type,
    searchTerm,
    currentPeriodKey,
    // Only the recent window moves with the day; a year, a month or a search
    // keeps the pages it has loaded.
    !searching && period.kind === "recent" ? recentSince : "",
  ]);

  useEffect(() => {
    if (error instanceof AuthExpiredError) onSessionExpired();
  }, [error, onSessionExpired]);
  const loadError =
    error !== null && !(error instanceof AuthExpiredError)
      ? t("artifacts.loadFailed")
      : "";

  // The summary (older count, months with artifacts) always counts against the
  // recent window, whatever period is shown, and belongs to one type filter:
  // another type's older count must not speak for this one. A failed one
  // leaves the page as is (the period bar then offers the recent window only;
  // a failed refresh keeps the last good one). summary null = it failed with
  // no good one for this type: nothing is known about older artifacts.
  const [summaryState, setSummaryState] = useState<{
    type: ArtifactListType;
    summary: ArtifactSummary | null;
  } | null>(null);
  const [summaryVersion, setSummaryVersion] = useState(0);
  useEffect(() => {
    let cancelled = false;
    getArtifactSummary({ since: recentSince, type })
      .then((next) => {
        if (!cancelled) setSummaryState({ type, summary: next });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof AuthExpiredError) onSessionExpiredRef.current();
        setSummaryState((prev) =>
          prev?.type === type ? prev : { type, summary: null },
        );
      });
    return () => {
      cancelled = true;
    };
  }, [recentSince, type, summaryVersion]);
  const summaryLoaded = summaryState?.type === type;
  const summary = summaryLoaded ? summaryState.summary : null;
  const summaryFailed = summaryLoaded && summary === null;

  // A recent window too big for one request comes in pages: tiles first would
  // put a later page's tiles above rows on screen, so it keeps arrival order
  // (to its end, or the finished window would rearrange itself).
  const mode: GroupMode = searching
    ? "flat"
    : period.kind === "recent"
      ? paged
        ? "recentPages"
        : "recent"
      : "months";
  const groups = useMemo(
    () => groupArtifacts(artifacts, now, mode),
    [artifacts, now, mode],
  );

  // groupCount is a heading's count: a calendar month's from the summary (this
  // type's, cut in this zone), so it holds before every page is in; else what
  // is loaded, once nothing more can load; else none rather than a low one.
  function groupCount(group: ArtifactGroup): number | null {
    if (group.kind === "calendarMonth") {
      const known = summary?.months.find((m) => m.month === group.key);
      if (known !== undefined) return known.count;
    }
    return hasMore ? null : group.size;
  }
  const inRecentWindow = !searching && period.kind === "recent";
  const olderCount = summary?.olderCount ?? 0;
  const olderYear =
    summary === null
      ? null
      : newestOlderYear(summary, new Date(recentSince).getFullYear());

  function selectPeriod(next: ArtifactPeriod) {
    setPeriod(next);
    // A period is a choice to browse, so it ends a search.
    changeSearch("");
    setOpenMenuID(null);
  }

  // Translate a failed artifact action into either a session expiry (bubbled up)
  // or an inline modal error.
  function reportActionError(err: unknown, fallback: string) {
    if (err instanceof AuthExpiredError) onSessionExpired();
    else setActionError(fallback);
  }

  async function handleRename(displayFilename: string) {
    if (renameTarget === null) return;
    setActionPending(true);
    setActionError("");
    try {
      await renameArtifact(renameTarget.id, displayFilename);
      // Reflect the new name in place; the chat transcript updates server-side
      // via the read-time overlay, so no refetch is needed here.
      setItems((prev) =>
        prev.map((item) =>
          item.id === renameTarget.id ? { ...item, displayFilename } : item,
        ),
      );
      setRenameTarget(null);
    } catch (err) {
      reportActionError(err, t("artifacts.renameFailed"));
    } finally {
      setActionPending(false);
    }
  }

  async function handleDelete() {
    if (deleteTarget === null) return;
    setActionPending(true);
    setActionError("");
    try {
      await deleteArtifact(deleteTarget.id);
      setItems((prev) => prev.filter((item) => item.id !== deleteTarget.id));
      setDeleteTarget(null);
      setSummaryVersion((version) => version + 1);
    } catch (err) {
      reportActionError(err, t("artifacts.deleteFailed"));
    } finally {
      setActionPending(false);
    }
  }

  function menuProps(artifact: Artifact): ArtifactItemMenuProps {
    const chat = chatOf(artifact);
    return {
      menuOpen: openMenuID === artifact.id,
      onToggleMenu: () =>
        setOpenMenuID((prev) => (prev === artifact.id ? null : artifact.id)),
      onCloseMenu: () =>
        setOpenMenuID((prev) => (prev === artifact.id ? null : prev)),
      // Only image artifacts can be re-referenced in a new chat without
      // re-uploading; gate on the same predicate the send path uses
      // (isImageAttachment) and skip deleted ones.
      onUseInThread:
        isImageAttachment({
          mimeType: artifact.mimeType,
          filename: artifact.displayFilename,
        }) && artifact.deleted !== true
          ? () => onUseInThread(artifact)
          : undefined,
      onOpenChat: chat === null ? undefined : () => onOpenThread(chat.threadId),
      onDownload: () => void handleDownload(artifact),
      onRequestRename: () => {
        setActionError("");
        setRenameTarget(artifact);
      },
      onRequestDelete: () => {
        setActionError("");
        setDeleteTarget(artifact);
      },
    };
  }

  function groupLabel(group: ArtifactGroup): string | null {
    switch (group.kind) {
      case "today":
        return t("artifacts.groupToday");
      case "week":
        return t("artifacts.groupWeek");
      case "month":
        return t("artifacts.groupMonth");
      case "calendarMonth":
        return formatMonth(group.year ?? 0, group.month ?? 1, i18n.language);
      case "all":
        return null;
    }
  }

  // The empty text for the recent window depends on the summary (older items
  // or none at all), so it waits for both rather than flash the wrong one.
  const showEmpty =
    artifacts.length === 0 &&
    loadError === "" &&
    loaded &&
    (!inRecentWindow || summaryLoaded);
  // Without a summary nothing is known about older artifacts, so the empty
  // window says only that it is empty, never that there are none at all.
  const emptyText = searching
    ? t("artifacts.noMatch")
    : period.kind !== "recent"
      ? t("artifacts.emptyPeriod")
      : olderCount > 0 || summaryFailed
        ? t("artifacts.emptyRecent")
        : t("artifacts.empty");
  // The older note goes below the whole window only: while the window still
  // loads, "not loaded" would speak of its own pages too.
  const showOlderNote =
    inRecentWindow && artifacts.length > 0 && olderCount > 0 && !hasMore;
  const showYearButton = olderYear !== null && (
    <button
      type="button"
      className="ui-meta-text mt-1 rounded-md px-1.5 py-0.5 font-medium text-ink-dim underline-offset-2 hover:text-ink hover:underline"
      onClick={() => selectPeriod({ kind: "year", year: olderYear })}
    >
      {t("artifacts.showYear", { year: olderYear })}
    </button>
  );

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="relative mx-auto w-full max-w-[802px] px-4 pb-16 pt-10 md:px-6">
        <SidebarOpenButton variant="floating" onClick={onOpenSidebar} />
        <header className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <h1 className="font-serif text-[28px] font-medium leading-8 text-ink-bright">
              {t("artifacts.title")}
            </h1>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <FilterButton
              active={type === "all"}
              label={t("artifacts.filterAll")}
              onClick={() => setType("all")}
            />
            <FilterButton
              active={type === "images"}
              label={t("artifacts.filterImages")}
              onClick={() => setType("images")}
            />
            <FilterButton
              active={type === "files"}
              label={t("artifacts.filterFiles")}
              onClick={() => setType("files")}
            />
          </div>
        </header>

        <div className="relative mt-6">
          <Icon
            name="search"
            size="18px"
            className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-muted"
          />
          <input
            type="text"
            autoFocus
            value={searchInput}
            onChange={(event) => changeSearch(event.target.value)}
            placeholder={t("artifacts.searchPlaceholder")}
            aria-label={t("artifacts.searchLabel")}
            className="ui-composer-text h-11 w-full rounded-xl border border-border-strong bg-track pl-11 pr-3 text-ink outline-none placeholder:text-muted focus:border-faint"
          />
        </div>

        <PeriodBar
          period={period}
          // The last months shown stay while a type switch loads its own,
          // rather than the chips blinking out and back.
          months={summaryState?.summary?.months ?? []}
          searching={searching}
          onChange={selectPeriod}
        />

        {(loadError || downloadError) !== "" && (
          <div
            role="alert"
            className="ui-meta-text mt-4 rounded-md border border-accent px-3 py-2 text-accent"
          >
            {loadError || downloadError}
          </div>
        )}

        <div className="mt-2">
          {showEmpty && (
            <div className="py-10 text-center text-muted">
              <div>{emptyText}</div>
              {inRecentWindow && olderCount > 0 && showYearButton}
            </div>
          )}
          {groups.map((group) => (
            <ArtifactGroupView
              key={group.key}
              group={group}
              ageInLine={mode === "flat"}
              label={groupLabel(group)}
              count={groupCount(group)}
              hoveredArtifactID={hoveredArtifactID}
              onHoverChange={(id, hovered) =>
                setHoveredArtifactID((prev) =>
                  hovered ? id : prev === id ? null : prev,
                )
              }
              menuProps={menuProps}
              onOpenThread={onOpenThread}
            />
          ))}
          {/* Sentinel observed for infinite scroll; loads the next page when in view. */}
          <div ref={sentinelRef} aria-hidden="true" className="h-px" />
          {loadingMore && hasMore && (
            <div className="ui-meta-text mt-3 px-1.5 text-muted">
              {t("artifacts.loadingMore")}
            </div>
          )}
          {showOlderNote && (
            <div className="ui-meta-text mt-8 border-t border-border px-1.5 pt-4 text-center text-muted">
              <div>{t("artifacts.olderNote", { count: olderCount })}</div>
              {showYearButton}
            </div>
          )}
        </div>
      </div>

      {renameTarget !== null && (
        <RenameArtifactModal
          filename={renameTarget.displayFilename}
          error={actionError}
          disabled={actionPending}
          onCancel={() => setRenameTarget(null)}
          onSubmit={(displayFilename) => void handleRename(displayFilename)}
        />
      )}
      {deleteTarget !== null && (
        <DeleteArtifactModal
          filename={deleteTarget.displayFilename}
          error={actionError}
          disabled={actionPending}
          onCancel={() => setDeleteTarget(null)}
          onDelete={() => void handleDelete()}
        />
      )}
    </div>
  );
}

// ArtifactGroupView is one group: a heading with its count (none for search
// results, nor while it is unknown), then its runs, images as a tile grid and
// other files as rows. Each tile run is its own grid, so the column-anchored
// menu placement (.ui-artifact-tile:nth-child) counts from its first tile.
function ArtifactGroupView({
  group,
  ageInLine,
  label,
  count,
  hoveredArtifactID,
  onHoverChange,
  menuProps,
  onOpenThread,
}: {
  group: ArtifactGroup;
  // Search results: no heading tells a row's age (ArtifactRow ageInLine).
  ageInLine: boolean;
  label: string | null;
  count: number | null;
  hoveredArtifactID: string | null;
  onHoverChange(id: string, hovered: boolean): void;
  menuProps(artifact: Artifact): ArtifactItemMenuProps;
  onOpenThread(threadId: string): void;
}) {
  const { t } = useTranslation();
  const headingID = useId();
  const body = group.runs.map((run, runIndex) => {
    const spacing = runIndex > 0 ? "mt-3" : "";
    if (run.kind === "tiles")
      return (
        <ul
          key={run.key}
          className={`grid grid-cols-3 gap-2 sm:grid-cols-5 sm:gap-3 ${spacing}`}
        >
          {run.items.map((artifact) => (
            <ArtifactTile
              key={artifact.id}
              artifact={artifact}
              onOpenThread={onOpenThread}
              {...menuProps(artifact)}
            />
          ))}
        </ul>
      );
    return (
      <ul key={run.key} className={spacing}>
        {run.items.map((artifact, index) => {
          const next = run.items[index + 1];
          const hovered = hoveredArtifactID === artifact.id;
          const nextHovered =
            next !== undefined && hoveredArtifactID === next.id;
          const menu = menuProps(artifact);
          return (
            <ArtifactRow
              key={artifact.id}
              artifact={artifact}
              ageInLine={ageInLine}
              hovered={hovered}
              hideDivider={hovered || nextHovered || menu.menuOpen}
              onHoverChange={(hovered) => onHoverChange(artifact.id, hovered)}
              onOpenThread={onOpenThread}
              {...menu}
            />
          );
        })}
      </ul>
    );
  });
  if (label === null) return <div className="mt-3">{body}</div>;
  return (
    <section aria-labelledby={headingID} className="mt-6">
      <div className="mb-2 flex items-baseline justify-between gap-3 px-1.5">
        <h2
          id={headingID}
          className="ui-control-text font-medium text-ink-warm"
        >
          {label}
        </h2>
        {count !== null && (
          <span className="ui-meta-text shrink-0 text-faint">
            {t("artifacts.groupCount", { count })}
          </span>
        )}
      </div>
      {body}
    </section>
  );
}

function FilterButton({
  active,
  label,
  onClick,
}: {
  active: boolean;
  label: string;
  onClick(): void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      className={`ui-control-text rounded-lg px-3 py-1.5 font-medium transition-colors ${
        active
          ? "bg-track text-ink-bright"
          : "bg-raised text-ink-dim hover:text-ink"
      }`}
      onClick={onClick}
    >
      {label}
    </button>
  );
}
