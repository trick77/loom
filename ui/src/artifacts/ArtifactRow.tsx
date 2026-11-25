import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { Artifact } from "../api";
import { BrowsingListRowFrame } from "../BrowsingListRowFrame";
import { formatFileSize } from "../chat/artifacts";
import { Icon } from "../chat/Icon";
import { PdfLightbox } from "../chat/PdfLightbox";
import { useEscapeKey } from "../chat/useEscapeKey";
import { useOutsideRefPointerDown } from "../chat/useOutsidePointerDown";
import { isPdfAttachment } from "../chat/useDocumentAttachments";
import { AttachmentPreview } from "../components/AttachmentPreview";
import { formatTimeAgo } from "../timeago";
import {
  ArtifactItemActionsMenu,
  type ArtifactItemMenuProps,
} from "./ArtifactActionsMenu";

// chatOf is the chat an artifact links to, or null when it has none to show.
export function chatOf(
  artifact: Artifact,
): { threadId: string; title: string } | null {
  const title = artifact.threadTitle ?? "";
  if (
    artifact.threadId === undefined ||
    artifact.threadId === "" ||
    title === ""
  )
    return null;
  return { threadId: artifact.threadId, title };
}

// ArtifactRow is a non-image artifact: icon, filename, its chat (or its type
// when it has none), age, size and the ⋮ menu. A PDF opens an inline preview,
// anything else downloads; a click anywhere on the row does the same as the
// filename button. A deleted PDF has no bytes to preview, so it downloads (and
// surfaces the gone state) instead of opening a doomed preview.
export function ArtifactRow({
  artifact,
  ageInLine = false,
  hideDivider,
  hovered,
  onHoverChange,
  onOpenThread,
  ...menu
}: {
  artifact: Artifact;
  // No group heading tells the row's age (search results): a phone, which has
  // no age column, shows it in the second line.
  ageInLine?: boolean;
  hideDivider: boolean;
  hovered: boolean;
  onHoverChange(hovered: boolean): void;
  onOpenThread(threadId: string): void;
} & ArtifactItemMenuProps) {
  const { t } = useTranslation();
  const [previewOpen, setPreviewOpen] = useState(false);
  const rowRef = useRef<HTMLLIElement | null>(null);
  const { menuOpen, onToggleMenu, onCloseMenu } = menu;
  const filename = artifact.displayFilename;
  const previewable =
    artifact.deleted !== true &&
    isPdfAttachment({ mimeType: artifact.mimeType, filename });
  const primary = previewable ? () => setPreviewOpen(true) : menu.onDownload;
  const chat = chatOf(artifact);
  const age = formatTimeAgo(artifact.modifiedAt ?? "");
  const lineAge = ageInLine && age !== "" && (
    <>
      <span className="shrink-0 whitespace-nowrap sm:hidden">
        {age}
        <span aria-hidden="true"> ·</span>
      </span>{" "}
    </>
  );

  // Close the menu on an outside click or Escape, mirroring the thread row menu.
  useEscapeKey(onCloseMenu, { active: menuOpen });
  useOutsideRefPointerDown(menuOpen, rowRef, onCloseMenu);

  return (
    <>
      <BrowsingListRowFrame
        ref={rowRef}
        active={hovered || menuOpen}
        after={menuOpen ? <ArtifactItemActionsMenu {...menu} /> : null}
        hideDivider={hideDivider}
        rowClassName="group"
        surfaceClassName="ui-artifacts-row-surface relative min-h-[56px] cursor-pointer rounded-xl px-1.5 py-2 transition-colors hover:bg-raised"
        onPointerEnter={() => onHoverChange(true)}
        onPointerLeave={() => onHoverChange(false)}
        onSurfaceClick={primary}
      >
        {/* A phone has no room for the age beside the name and chat title; the
            group heading already says how old the row is, or the second line
            does (ageInLine). */}
        <div className="ui-artifacts-row-primary grid grid-cols-[minmax(0,1fr)_4.5rem_2rem] items-center gap-0 sm:grid-cols-[minmax(0,1fr)_10rem_7rem_2rem]">
          <div className="flex min-w-0 items-start gap-3 pr-3">
            <AttachmentPreview
              mimeType={artifact.mimeType}
              filename={filename}
              className="grid h-9 w-9 shrink-0 place-items-center rounded-md bg-track text-ink-dim"
            />
            <span className="block min-w-0">
              <button
                type="button"
                aria-label={t(
                  previewable ? "artifacts.preview" : "artifacts.download",
                  { filename },
                )}
                className="block max-w-full truncate text-left text-[15px] leading-5 text-ink-bright"
                onClick={(event) => {
                  event.stopPropagation();
                  primary();
                }}
              >
                {filename}
              </button>
              {chat === null ? (
                <span className="ui-artifacts-row-secondary flex min-w-0 gap-1 text-xs leading-4 text-muted">
                  {lineAge}
                  <span className="min-w-0 truncate">{artifact.mimeType}</span>
                </span>
              ) : (
                // A flex line, so a long chat title truncates itself rather
                // than the whole button turning into an ellipsis.
                <span className="ui-artifacts-row-secondary flex min-w-0 gap-1 text-xs leading-4 text-muted">
                  {lineAge}
                  <span className="shrink-0">
                    {t("artifacts.fromChat")}
                  </span>{" "}
                  <button
                    type="button"
                    aria-label={t("artifacts.openChat", { title: chat.title })}
                    className="min-w-0 truncate text-left text-ink-dim underline-offset-2 hover:text-ink hover:underline"
                    onClick={(event) => {
                      event.stopPropagation();
                      onOpenThread(chat.threadId);
                    }}
                  >
                    {chat.title}
                  </button>
                </span>
              )}
            </span>
          </div>
          <div className="hidden shrink-0 text-[13px] text-muted sm:block">
            {age}
          </div>
          <div className="shrink-0 text-[13px] text-ink-dim">
            {formatFileSize(artifact.sizeBytes)}
          </div>
          {/* The actions button has its own reserved column so the size stays put
              and visible; the button only toggles visibility (keeping its slot) on
              hover or keyboard focus in the row, and is permanently shown on touch. */}
          <button
            aria-expanded={menuOpen}
            aria-label={t("artifacts.actionsFor", { filename })}
            className={`grid h-7 w-7 place-items-center justify-self-end rounded-md text-ink-warm transition-colors hover:bg-track hover:text-ink ${
              hovered || menuOpen
                ? ""
                : "invisible group-focus-within:visible [@media(hover:none)]:visible"
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
      {previewOpen && (
        <PdfLightbox
          downloadUrl={artifact.downloadUrl}
          filename={filename}
          onClose={() => setPreviewOpen(false)}
        />
      )}
    </>
  );
}
