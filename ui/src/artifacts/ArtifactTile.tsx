import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { Artifact } from "../api";
import { Icon } from "../chat/Icon";
import { ImageLightbox } from "../chat/ImageLightbox";
import { useEscapeKey } from "../chat/useEscapeKey";
import { useOutsideRefPointerDown } from "../chat/useOutsidePointerDown";
import { AttachmentPreview } from "../components/AttachmentPreview";
import {
  ArtifactItemActionsMenu,
  type ArtifactItemMenuProps,
} from "./ArtifactActionsMenu";
import { chatOf } from "./ArtifactRow";

// ArtifactTile is an image artifact in the tile grid: its thumbnail opens the
// lightbox. Filename and chat show on hover or focus (an overlay), on touch as
// a caption under the tile; the ⋮ menu likewise (see .ui-artifact-tile* in
// index.css).
export function ArtifactTile({
  artifact,
  onOpenThread,
  ...menu
}: {
  artifact: Artifact;
  onOpenThread(threadId: string): void;
} & ArtifactItemMenuProps) {
  const { t } = useTranslation();
  const [previewOpen, setPreviewOpen] = useState(false);
  const tileRef = useRef<HTMLLIElement | null>(null);
  const { menuOpen, onToggleMenu, onCloseMenu } = menu;
  const filename = artifact.displayFilename;
  const chat = chatOf(artifact);

  useEscapeKey(onCloseMenu, { active: menuOpen });
  useOutsideRefPointerDown(menuOpen, tileRef, onCloseMenu);

  return (
    <li ref={tileRef} className="ui-artifact-tile relative min-w-0">
      <button
        type="button"
        aria-label={t("artifacts.preview", { filename })}
        className="relative block aspect-square w-full overflow-hidden rounded-xl bg-card outline-none focus-visible:ring-2 focus-visible:ring-accent"
        onClick={() => setPreviewOpen(true)}
      >
        <AttachmentPreview
          mimeType={artifact.mimeType}
          filename={filename}
          previewUrl={artifact.thumbnailUrl ?? artifact.downloadUrl}
          alt={t("artifacts.thumbnailAlt", { filename })}
          className="h-full w-full text-ink-dim"
        />
        <span
          aria-hidden="true"
          className="ui-artifact-tile-overlay pointer-events-none absolute inset-x-0 bottom-0 px-2 pb-1.5 pt-6 text-left"
        >
          <span className="block truncate text-xs font-medium leading-4 text-ink">
            {filename}
          </span>
          {chat !== null && (
            <span className="block truncate text-[11px] leading-4 text-muted">
              {chat.title}
            </span>
          )}
        </span>
      </button>
      <div className="ui-artifact-tile-caption mt-1 min-w-0 px-0.5">
        <span className="block truncate text-xs leading-4 text-ink-soft">
          {filename}
        </span>
        {chat !== null && (
          <button
            type="button"
            aria-label={t("artifacts.openChat", { title: chat.title })}
            className="block max-w-full truncate text-left text-[11px] leading-4 text-muted"
            onClick={() => onOpenThread(chat.threadId)}
          >
            {chat.title}
          </button>
        )}
      </div>
      <button
        aria-expanded={menuOpen}
        aria-label={t("artifacts.actionsFor", { filename })}
        className="ui-artifact-tile-kebab absolute right-1.5 top-1.5 grid h-7 w-7 place-items-center rounded-md bg-bg/75 text-ink-soft backdrop-blur-sm transition-colors hover:text-ink"
        onClick={onToggleMenu}
        type="button"
      >
        <Icon name="moreVertical" size="18px" />
      </button>
      {menuOpen && (
        <ArtifactItemActionsMenu className="ui-artifact-tile-menu" {...menu} />
      )}
      {previewOpen && (
        <ImageLightbox
          src={artifact.downloadUrl}
          alt={filename}
          onClose={() => setPreviewOpen(false)}
        />
      )}
    </li>
  );
}
