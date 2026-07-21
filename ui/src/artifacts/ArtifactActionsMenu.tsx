import { useTranslation } from "react-i18next";

import { DownloadIcon } from "../chat/icons";
import { Icon } from "../chat/Icon";
import { useMenuPlacement } from "../chat/useMenuPlacement";
import {
  menuDeleteItemClass,
  menuIconClass,
  menuItemClass,
  TrashMenuIcon,
} from "../ThreadActionsMenu";

// ArtifactActionsMenu is the per-row context menu for the Artifacts library:
// Download, Rename, then optionally "Use in thread", a divider, then Delete. It
// reuses the shared menu styling so the hover highlight, sizing, and
// destructive-delete treatment match every other menu. "Use in thread" only
// appears when onUseInThread is provided (image artifacts that can be
// re-referenced in a new chat without re-uploading — see ArtifactsPage for the
// gating).
export function ArtifactActionsMenu({
  onDownload,
  onUseInThread,
  onRename,
  onDelete,
}: {
  onDownload(): void;
  onUseInThread?(): void;
  onRename(): void;
  onDelete(): void;
}) {
  const { t } = useTranslation();
  // Anchored 44px down from the row top (below the ⋮). Mirror that offset when
  // flipping up so a bottom-row menu isn't clipped by the end of the list.
  const { menuRef, dropUp } = useMenuPlacement();
  return (
    <div
      ref={menuRef}
      aria-label={t("artifacts.menuLabel")}
      className={`ui-sidebar-text absolute right-3 ${dropUp ? "bottom-11" : "top-11"} z-20 w-[168px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] py-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)]`}
      role="menu"
    >
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={onDownload}
      >
        <span
          className={`${menuIconClass} text-[19px] leading-none`}
          aria-hidden="true"
        >
          <DownloadIcon />
        </span>
        {t("artifacts.menuDownload")}
      </button>
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={onRename}
      >
        <span
          className={`${menuIconClass} text-[19px] leading-none`}
          aria-hidden="true"
        >
          <Icon name="edit" size="19px" />
        </span>
        {t("artifacts.menuRename")}
      </button>
      {onUseInThread !== undefined && (
        <button
          className={`${menuItemClass} text-[#f3f0e8]`}
          role="menuitem"
          type="button"
          onClick={onUseInThread}
        >
          <span
            className={`${menuIconClass} text-[19px] leading-none`}
            aria-hidden="true"
          >
            <Icon name="copy" size="19px" />
          </span>
          {t("artifacts.menuUseInThread")}
        </button>
      )}
      <div className="mx-[14px] my-[5px] h-px bg-[#4a4741]" role="separator" />
      <button
        className={menuDeleteItemClass}
        role="menuitem"
        type="button"
        onClick={onDelete}
      >
        <TrashMenuIcon />
        {t("common.delete")}
      </button>
    </div>
  );
}
