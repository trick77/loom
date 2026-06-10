import { useEffect, useMemo, useState, type ReactNode } from "react";

import {
  AuthExpiredError,
  downloadArtifact,
  listArtifacts,
  type Artifact,
  type ArtifactListType,
  type ArtifactSort,
  type SortOrder,
} from "../api";
import { formatFileSize } from "../chat/artifacts";
import { Icon } from "../chat/Icon";
import { CloseIcon, FileIcon } from "../chat/icons";
import { SidebarOpenButton } from "../SidebarOpenButton";
import { formatTimeAgo } from "../timeago";

const PAGE_LIMIT = 1000;
const SEARCH_DEBOUNCE_MS = 250;

export function LibraryPage({
  onOpenSidebar,
  onSessionExpired,
}: {
  onOpenSidebar(): void;
  onSessionExpired(): void;
}) {
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const [searchTerm, setSearchTerm] = useState("");
  const [type, setType] = useState<ArtifactListType>("all");
  const [sort, setSort] = useState<ArtifactSort>("modified");
  const [order, setOrder] = useState<SortOrder>("desc");

  useEffect(() => {
    const handle = window.setTimeout(() => setSearchTerm(searchInput.trim()), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(handle);
  }, [searchInput]);

  useEffect(() => {
    let active = true;
    listArtifacts({ type, sort, order, search: searchTerm, limit: PAGE_LIMIT })
      .then((next) => {
        if (!active) return;
        setArtifacts(next);
        setLoaded(true);
        setLoadError("");
      })
      .catch((error: unknown) => {
        if (!active) return;
        if (error instanceof AuthExpiredError) {
          onSessionExpired();
          return;
        }
        setLoadError("Artifacts failed to load.");
      });
    return () => {
      active = false;
    };
  }, [onSessionExpired, order, searchTerm, sort, type]);

  function updateSort(nextSort: ArtifactSort) {
    if (sort === nextSort) {
      setOrder((current) => (current === "asc" ? "desc" : "asc"));
      return;
    }
    setSort(nextSort);
    setOrder(nextSort === "modified" ? "desc" : "asc");
  }

  const visibleArtifacts = useMemo(
    () => projectArtifacts(artifacts, { type, sort, order, search: searchTerm }),
    [artifacts, order, searchTerm, sort, type],
  );

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-[860px] px-4 pb-16 pt-10 md:px-6">
        <header className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <SidebarOpenButton onClick={onOpenSidebar} />
            <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">Library</h1>
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
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder="Search filenames..."
            aria-label="Search filenames"
            className="slopr-composer-text h-11 w-full rounded-xl border border-[#3f3f3d] bg-[#343433] pl-11 pr-3 text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]"
          />
        </div>

        {loadError !== "" && (
          <div className="slopr-meta-text mt-4 rounded-md border border-accent px-3 py-2 text-accent">
            {loadError}
          </div>
        )}

        <div className="mt-3">
          <div className="grid min-h-8 grid-cols-[minmax(0,1fr)_8.5rem_5.5rem] items-center border-b border-[#343432] px-1.5 text-xs font-semibold text-[#aaa79e] sm:grid-cols-[minmax(0,1fr)_10rem_7rem]">
            <SortButton active={sort === "name"} label="Name" order={order} onClick={() => updateSort("name")} />
            <SortButton active={sort === "modified"} label="Modified" order={order} onClick={() => updateSort("modified")} />
            <SortButton active={sort === "size"} label="Size" order={order} onClick={() => updateSort("size")} />
          </div>
          {visibleArtifacts.length === 0 && loadError === "" ? (
            loaded && (
              <div className="py-10 text-center text-[#807d74]">
                {searchTerm === "" ? "No artifacts yet." : "No artifacts match your search."}
              </div>
            )
          ) : (
            <ul>
              {visibleArtifacts.map((artifact) => (
                <ArtifactRow key={artifact.id} artifact={artifact} />
              ))}
            </ul>
          )}
          {loaded && artifacts.length >= PAGE_LIMIT && (
            <div className="slopr-meta-text mt-3 px-1.5 text-[#8a887f]">
              Showing the latest {PAGE_LIMIT} artifacts.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function projectArtifacts(
  artifacts: Artifact[],
  opts: { type: ArtifactListType; sort: ArtifactSort; order: SortOrder; search: string },
) {
  const search = opts.search.trim().toLowerCase();
  const direction = opts.order === "asc" ? 1 : -1;
  return artifacts
    .filter((artifact) => {
      const isImage = artifact.mimeType.startsWith("image/");
      if (opts.type === "images" && !isImage) return false;
      if (opts.type === "files" && isImage) return false;
      if (search !== "" && !artifact.displayFilename.toLowerCase().includes(search)) return false;
      return true;
    })
    .sort((a, b) => {
      let result = 0;
      if (opts.sort === "name") {
        result = a.displayFilename.localeCompare(b.displayFilename, undefined, { sensitivity: "base" });
      } else if (opts.sort === "size") {
        result = a.sizeBytes - b.sizeBytes;
      } else {
        result = timeValue(a.modifiedAt) - timeValue(b.modifiedAt);
      }
      if (result === 0) result = a.id.localeCompare(b.id);
      return result * direction;
    });
}

function timeValue(value: string | undefined) {
  if (value === undefined) return 0;
  const ms = new Date(value).getTime();
  return Number.isNaN(ms) ? 0 : ms;
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
      className={`slopr-control-text rounded-lg px-3 py-1.5 font-medium transition-colors ${
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

function ArtifactRow({ artifact }: { artifact: Artifact }) {
  const isImage = artifact.mimeType.startsWith("image/");
  if (isImage) return <ImageArtifactRow artifact={artifact} />;
  return (
    <ArtifactRowFrame artifact={artifact} action={<FileArtifactButton artifact={artifact} />} />
  );
}

function ArtifactRowFrame({
  action,
  artifact,
  ariaLabel,
  onClick,
}: {
  action: ReactNode;
  artifact: Artifact;
  ariaLabel?: string;
  onClick?: () => void;
}) {
  const modifiedAt = artifact.modifiedAt ?? "";
  const interactive = onClick !== undefined;
  return (
    <li className="relative border-b border-[#343432]">
      <div
        aria-label={ariaLabel}
        className={`slopr-library-row-surface min-h-[56px] rounded-md px-1.5 py-2 transition-colors hover:bg-[#2a2a28] ${
          interactive ? "cursor-pointer" : ""
        }`}
        onClick={onClick}
        onKeyDown={(event) => {
          if (!interactive) return;
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onClick();
          }
        }}
        role={interactive ? "button" : undefined}
        tabIndex={interactive ? 0 : undefined}
      >
        <div className="slopr-library-row-primary grid grid-cols-[minmax(0,1fr)_8.5rem_5.5rem] items-center gap-0 sm:grid-cols-[minmax(0,1fr)_10rem_7rem]">
          <div className="min-w-0 pr-3">{action}</div>
          <div className="shrink-0 text-[13px] text-[#8a887f]">{formatTimeAgo(modifiedAt)}</div>
          <div className="shrink-0 text-[13px] text-[#c7c5bd]">{formatFileSize(artifact.sizeBytes)}</div>
        </div>
        <div className="slopr-library-row-secondary ml-12 mt-0.5 truncate text-xs text-[#8a887f]">
          {artifact.mimeType}
        </div>
      </div>
    </li>
  );
}

function FileArtifactButton({ artifact }: { artifact: Artifact }) {
  return (
    <button
      type="button"
      className="flex w-full min-w-0 items-center gap-3 text-left"
      aria-label={`Download ${artifact.displayFilename}`}
      onClick={() => void downloadToBrowser(artifact)}
    >
      <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-[#3a3a37] text-[#c7c5bd]">
        <FileIcon />
      </span>
      <span className="block min-w-0 truncate text-[15px] text-[#ecece6]">{artifact.displayFilename}</span>
    </button>
  );
}

function ImageArtifactRow({ artifact }: { artifact: Artifact }) {
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
        onClick={openPreview}
        action={
          <div
            className="flex w-full min-w-0 items-center gap-3 text-left"
            title={`Preview ${artifact.displayFilename}`}
          >
            <span className="grid h-9 w-9 shrink-0 place-items-center overflow-hidden rounded-md bg-[#1f1f1d] text-[#c7c5bd]">
              <img
                className="h-full w-full object-cover"
                src={artifact.downloadUrl}
                alt={`${artifact.displayFilename} thumbnail`}
                loading="lazy"
              />
            </span>
            <span className="block min-w-0 truncate text-[15px] text-[#ecece6]">{artifact.displayFilename}</span>
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
            <CloseIcon />
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
