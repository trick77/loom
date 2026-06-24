import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import {
  AuthExpiredError,
  deleteArtifact,
  downloadArtifact,
  listArtifacts,
  renameArtifact,
  type Artifact,
  type ArtifactListType,
  type ArtifactSort,
  type SortOrder,
} from "../api";
import { BrowsingListRowFrame } from "../BrowsingListRowFrame";
import { formatFileSize } from "../chat/artifacts";
import { Icon } from "../chat/Icon";
import { isImageAttachment } from "../chat/useDocumentAttachments";
import { AttachmentPreview } from "../components/AttachmentPreview";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { formatTimeAgo } from "../timeago";
import { useInfiniteList } from "../useInfiniteList";
import { ArtifactActionsMenu } from "./ArtifactActionsMenu";
import { DeleteArtifactModal, RenameArtifactModal } from "./ArtifactActionModals";

const PAGE_SIZE = 50;
const SEARCH_DEBOUNCE_MS = 250;

export function ArtifactsPage({
  onOpenSidebar,
  onSessionExpired,
  onUseInThread,
}: {
  onOpenSidebar(): void;
  onSessionExpired(): void;
  // Start a new chat with this artifact pre-attached (no re-upload). Only wired
  // for artifacts that can be referenced as an image input.
  onUseInThread(artifact: Artifact): void;
}) {
  const [searchInput, setSearchInput] = useState("");
  const [searchTerm, setSearchTerm] = useState("");
  const [type, setType] = useState<ArtifactListType>("all");
  const [sort, setSort] = useState<ArtifactSort>("modified");
  const [order, setOrder] = useState<SortOrder>("desc");
  const [hoveredArtifactID, setHoveredArtifactID] = useState<string | null>(null);
  const [openMenuID, setOpenMenuID] = useState<string | null>(null);
  const [renameTarget, setRenameTarget] = useState<Artifact | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Artifact | null>(null);
  const [actionError, setActionError] = useState("");
  const [actionPending, setActionPending] = useState(false);

  useEffect(() => {
    const handle = window.setTimeout(() => setSearchTerm(searchInput.trim()), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(handle);
  }, [searchInput]);

  // The server owns filtering and ordering; the client renders pages in the
  // exact order they arrive so cursor boundaries stay aligned (no client re-sort).
  const fetchPage = useCallback(
    (cursor: string | null) =>
      listArtifacts({ type, sort, order, search: searchTerm, limit: PAGE_SIZE, cursor }),
    [type, sort, order, searchTerm],
  );
  const { items: artifacts, setItems, loaded, loadingMore, hasMore, error, sentinelRef } = useInfiniteList(
    fetchPage,
    [type, sort, order, searchTerm],
  );

  useEffect(() => {
    if (error instanceof AuthExpiredError) onSessionExpired();
  }, [error, onSessionExpired]);
  const loadError = error !== null && !(error instanceof AuthExpiredError) ? "Artifacts failed to load." : "";

  function updateSort(nextSort: ArtifactSort) {
    if (sort === nextSort) {
      setOrder((current) => (current === "asc" ? "desc" : "asc"));
      return;
    }
    setSort(nextSort);
    setOrder(nextSort === "modified" ? "desc" : "asc");
  }

  // Translate a failed artifact action into either a session expiry (bubbled up)
  // or an inline modal error; returns true when the error was a session expiry.
  function reportActionError(err: unknown, fallback: string): boolean {
    if (err instanceof AuthExpiredError) {
      onSessionExpired();
      return true;
    }
    setActionError(fallback);
    return false;
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
        prev.map((item) => (item.id === renameTarget.id ? { ...item, displayFilename } : item)),
      );
      setRenameTarget(null);
    } catch (err) {
      reportActionError(err, "Rename failed.");
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
    } catch (err) {
      reportActionError(err, "Delete failed.");
    } finally {
      setActionPending(false);
    }
  }

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-[802px] px-4 pb-16 pt-10 md:px-6">
        <header className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <SidebarOpenButton onClick={onOpenSidebar} />
            <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">Artifacts</h1>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <FilterButton active={type === "all"} label="All" onClick={() => setType("all")} />
            <FilterButton active={type === "images"} label="Images" onClick={() => setType("images")} />
            <FilterButton active={type === "files"} label="Files" onClick={() => setType("files")} />
          </div>
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
            placeholder="Search filenames..."
            aria-label="Search filenames"
            className="ui-composer-text h-11 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] pl-11 pr-3 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
          />
        </div>

        {loadError !== "" && (
          <div className="ui-meta-text mt-4 rounded-md border border-accent px-3 py-2 text-accent">
            {loadError}
          </div>
        )}

        <div className="mt-3">
          {artifacts.length > 0 && (
            <div
              className={`grid min-h-8 grid-cols-[minmax(0,1fr)_8.5rem_5.5rem_2rem] items-center border-b px-1.5 text-xs font-semibold text-[#aaa79e] sm:grid-cols-[minmax(0,1fr)_10rem_7rem_2rem] ${
                hoveredArtifactID === artifacts[0]?.id ? "border-transparent" : "border-[#343432]"
              }`}
            >
              <SortButton active={sort === "name"} label="Name" order={order} onClick={() => updateSort("name")} />
              <SortButton active={sort === "modified"} label="Modified" order={order} onClick={() => updateSort("modified")} />
              <SortButton active={sort === "size"} label="Size" order={order} onClick={() => updateSort("size")} />
            </div>
          )}
          {artifacts.length === 0 && loadError === "" ? (
            loaded && (
              <div className="py-10 text-center text-[#807d74]">
                {searchTerm === "" ? "No artifacts yet." : "No artifacts match your search."}
              </div>
            )
          ) : (
            <ul>
              {artifacts.map((artifact, index) => {
                const nextArtifact = artifacts[index + 1];
                const hovered = hoveredArtifactID === artifact.id;
                const nextHovered = nextArtifact !== undefined && hoveredArtifactID === nextArtifact.id;
                const menuOpen = openMenuID === artifact.id;
                return (
                  <ArtifactRow
                    key={artifact.id}
                    artifact={artifact}
                    hovered={hovered}
                    hideDivider={hovered || nextHovered || menuOpen}
                    onHoverChange={(hovered) => setHoveredArtifactID(hovered ? artifact.id : null)}
                    menuOpen={menuOpen}
                    onToggleMenu={() => setOpenMenuID((prev) => (prev === artifact.id ? null : artifact.id))}
                    onCloseMenu={() => setOpenMenuID((prev) => (prev === artifact.id ? null : prev))}
                    onUseInThread={
                      // Only image artifacts can be re-referenced in a new chat
                      // without re-uploading; gate on the same predicate the send
                      // path uses (isImageAttachment) and skip deleted ones.
                      isImageAttachment({ mimeType: artifact.mimeType, filename: artifact.displayFilename }) &&
                      artifact.deleted !== true
                        ? () => onUseInThread(artifact)
                        : undefined
                    }
                    onRequestRename={() => {
                      setActionError("");
                      setRenameTarget(artifact);
                    }}
                    onRequestDelete={() => {
                      setActionError("");
                      setDeleteTarget(artifact);
                    }}
                  />
                );
              })}
            </ul>
          )}
          {/* Sentinel observed for infinite scroll; loads the next page when in view. */}
          <div ref={sentinelRef} aria-hidden="true" className="h-px" />
          {loadingMore && hasMore && (
            <div className="ui-meta-text mt-3 px-1.5 text-[#8a887f]">Loading more…</div>
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
        active ? "bg-[#343433] text-[#f5f3ee]" : "bg-[#282827] text-[#c7c5bd] hover:text-white"
      }`}
      onClick={onClick}
    >
      {label}
    </button>
  );
}

function SortButton({
  active,
  label,
  order,
  onClick,
}: {
  active: boolean;
  label: string;
  order: SortOrder;
  onClick(): void;
}) {
  return (
    <button
      type="button"
      className={`flex items-center gap-1 text-left transition-colors hover:text-[#f4f0e8] ${
        active ? "text-[#dedbd0]" : ""
      }`}
      onClick={onClick}
    >
      <span>{label}</span>
      {active && <Icon name={order === "asc" ? "sortUp" : "sortDown"} size="0.9em" />}
    </button>
  );
}

type ArtifactRowMenuProps = {
  menuOpen: boolean;
  onToggleMenu(): void;
  onCloseMenu(): void;
  // Present only for artifacts that can be re-referenced in a new chat without
  // re-uploading (image artifacts); undefined hides the "Use in thread" entry.
  onUseInThread?(): void;
  onRequestRename(): void;
  onRequestDelete(): void;
};

function ArtifactRow({
  artifact,
  hideDivider,
  hovered,
  onHoverChange,
  ...menu
}: {
  artifact: Artifact;
  hideDivider: boolean;
  hovered: boolean;
  onHoverChange(hovered: boolean): void;
} & ArtifactRowMenuProps) {
  const isImage = artifact.mimeType.startsWith("image/");
  if (isImage) {
    return (
      <ImageArtifactRow
        artifact={artifact}
        hideDivider={hideDivider}
        hovered={hovered}
        onHoverChange={onHoverChange}
        {...menu}
      />
    );
  }
  return (
    <ArtifactRowFrame
      artifact={artifact}
      action={<FileArtifactButton artifact={artifact} />}
      hideDivider={hideDivider}
      hovered={hovered}
      onHoverChange={onHoverChange}
      {...menu}
    />
  );
}

function ArtifactRowFrame({
  action,
  artifact,
  ariaLabel,
  hideDivider,
  hovered,
  onClick,
  onHoverChange,
  menuOpen,
  onToggleMenu,
  onCloseMenu,
  onUseInThread,
  onRequestRename,
  onRequestDelete,
}: {
  action: ReactNode;
  artifact: Artifact;
  ariaLabel?: string;
  hideDivider: boolean;
  hovered: boolean;
  onClick?: () => void;
  onHoverChange(hovered: boolean): void;
} & ArtifactRowMenuProps) {
  const modifiedAt = artifact.modifiedAt ?? "";
  const interactive = onClick !== undefined;
  const rowRef = useRef<HTMLLIElement | null>(null);
  const showMenuButton = hovered || menuOpen;

  // Close the menu on an outside click or Escape, mirroring the thread row menu.
  useEffect(() => {
    if (!menuOpen) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Node) || rowRef.current?.contains(target)) return;
      onCloseMenu();
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onCloseMenu();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [menuOpen, onCloseMenu]);

  return (
    <BrowsingListRowFrame
      ref={rowRef}
      active={hovered || menuOpen}
      after={
        menuOpen ? (
          <ArtifactActionsMenu
            onUseInThread={
              onUseInThread === undefined
                ? undefined
                : () => {
                    onCloseMenu();
                    onUseInThread();
                  }
            }
            onRename={() => {
              onCloseMenu();
              onRequestRename();
            }}
            onDelete={() => {
              onCloseMenu();
              onRequestDelete();
            }}
          />
        ) : null
      }
      hideDivider={hideDivider}
      surfaceAriaLabel={ariaLabel}
      surfaceClassName={`ui-artifacts-row-surface relative min-h-[56px] rounded-xl px-1.5 py-2 transition-colors hover:bg-[#2a2a28] ${
        interactive ? "cursor-pointer" : ""
      }`}
      surfaceRole={interactive ? "button" : undefined}
      surfaceTabIndex={interactive ? 0 : undefined}
      onPointerEnter={() => onHoverChange(true)}
      onPointerLeave={() => onHoverChange(false)}
      onSurfaceClick={onClick}
      onSurfaceKeyDown={(event) => {
        if (!interactive) return;
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onClick();
        }
      }}
    >
      <div className="ui-artifacts-row-primary grid grid-cols-[minmax(0,1fr)_8.5rem_5.5rem_2rem] items-center gap-0 sm:grid-cols-[minmax(0,1fr)_10rem_7rem_2rem]">
        <div className="min-w-0 pr-3">{action}</div>
        <div className="shrink-0 text-[13px] text-[#8a887f]">{formatTimeAgo(modifiedAt)}</div>
        <div className="shrink-0 text-[13px] text-[#c7c5bd]">{formatFileSize(artifact.sizeBytes)}</div>
        {/* The actions button has its own reserved column so the size stays put
            and visible; the button only toggles visibility (keeping its slot) on
            hover, and is permanently shown on touch. */}
        <button
          aria-expanded={menuOpen}
          aria-label={`Actions for ${artifact.displayFilename}`}
          className={`grid h-7 w-7 place-items-center justify-self-end rounded-md text-[#d8d4ca] transition-colors hover:bg-[#363632] hover:text-white ${
            showMenuButton ? "" : "invisible [@media(hover:none)]:visible"
          }`}
          onClick={(event) => {
            event.stopPropagation();
            onToggleMenu();
          }}
          type="button"
        >
          <Icon name="moreVertical" size="18px" />
        </button>
      </div>
    </BrowsingListRowFrame>
  );
}

function FileArtifactButton({ artifact }: { artifact: Artifact }) {
  return (
    <button
      type="button"
      className="flex w-full min-w-0 items-start gap-3 text-left"
      aria-label={`Download ${artifact.displayFilename}`}
      onClick={() => void downloadToBrowser(artifact)}
    >
      <AttachmentPreview
        mimeType={artifact.mimeType}
        filename={artifact.displayFilename}
        className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]"
      />
      <span className="block min-w-0">
        <span className="block truncate text-[15px] leading-5 text-[#ecece6]">{artifact.displayFilename}</span>
        <span className="ui-artifacts-row-secondary block truncate text-xs leading-4 text-[#8a887f]">
          {artifact.mimeType}
        </span>
      </span>
    </button>
  );
}

function ImageArtifactRow({
  artifact,
  hideDivider,
  hovered,
  onHoverChange,
  ...menu
}: {
  artifact: Artifact;
  hideDivider: boolean;
  hovered: boolean;
  onHoverChange(hovered: boolean): void;
} & ArtifactRowMenuProps) {
  const [lightboxOpen, setLightboxOpen] = useState(false);

  useEffect(() => {
    if (!lightboxOpen) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setLightboxOpen(false);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [lightboxOpen]);

  const openPreview = () => {
    setLightboxOpen(true);
  };

  return (
    <>
      <ArtifactRowFrame
        ariaLabel={`Preview ${artifact.displayFilename}`}
        artifact={artifact}
        hideDivider={hideDivider}
        hovered={hovered}
        onHoverChange={onHoverChange}
        onClick={openPreview}
        {...menu}
        action={
          <div
            className="flex w-full min-w-0 items-start gap-3 text-left"
            title={`Preview ${artifact.displayFilename}`}
          >
            <AttachmentPreview
              mimeType={artifact.mimeType}
              filename={artifact.displayFilename}
              previewUrl={artifact.downloadUrl}
              alt={`${artifact.displayFilename} thumbnail`}
              className="grid h-9 w-9 shrink-0 place-items-center overflow-hidden rounded-md bg-[#1f1f1d] text-[#c7c5bd]"
            />
            <span className="block min-w-0">
              <span className="block truncate text-[15px] leading-5 text-[#ecece6]">{artifact.displayFilename}</span>
              <span className="ui-artifacts-row-secondary block truncate text-xs leading-4 text-[#8a887f]">
                {artifact.mimeType}
              </span>
            </span>
          </div>
        }
      />
      {lightboxOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-6"
          onClick={() => setLightboxOpen(false)}
          role="dialog"
          aria-modal="true"
          aria-label={`Preview ${artifact.displayFilename}`}
        >
          <button
            className="absolute right-4 top-4 grid h-9 w-9 place-items-center rounded-md bg-black/40 text-[#f3f0e8] transition-colors hover:bg-black/60"
            onClick={() => setLightboxOpen(false)}
            type="button"
            title="Close preview"
            aria-label="Close preview"
          >
            <Icon name="close" size="20px" />
          </button>
          <img
            className="max-h-full max-w-full object-contain"
            src={artifact.downloadUrl}
            alt={artifact.displayFilename}
            onClick={(event) => event.stopPropagation()}
          />
        </div>
      )}
    </>
  );
}

async function downloadToBrowser(artifact: Artifact) {
  const blob = await downloadArtifact(artifact.downloadUrl);
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = artifact.displayFilename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}
