import { useTranslation } from "react-i18next";

import type { Project } from "../api";
import { Icon } from "../chat/Icon";
import { useMenuPlacement } from "../chat/useMenuPlacement";
import { menuDeleteItemClass, menuIconClass, menuItemClass, TrashMenuIcon } from "../ThreadActionsMenu";

export function ProjectActionsMenu({
  project,
  className = "right-0",
  archived = false,
  onEdit,
  onArchive,
  onUnarchive,
  onDelete,
}: {
  project: Project;
  className?: string;
  archived?: boolean;
  onEdit(project: Project): void;
  onArchive(project: Project): void;
  onUnarchive(project: Project): void;
  onDelete(project: Project): void;
}) {
  const { t } = useTranslation();
  const { menuRef, verticalClass } = useMenuPlacement();
  return (
    <div
      ref={menuRef}
      aria-label={t("projects.actions.menuLabel")}
      className={`ui-sidebar-text absolute z-20 ${verticalClass} w-[168px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] py-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)] ${className}`}
      role="menu"
    >
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => onEdit(project)}
      >
        <EditIcon />
        {t("projects.actions.editDetails")}
      </button>
      <div className="mx-[14px] my-[5px] h-px bg-[#454540]" role="separator" />
      <button
        className={`${menuItemClass} text-[#f3f0e8]`}
        role="menuitem"
        type="button"
        onClick={() => (archived ? onUnarchive : onArchive)(project)}
      >
        <ArchiveIcon />
        {archived ? t("projects.actions.unarchive") : t("projects.actions.archive")}
      </button>
      <button
        className={menuDeleteItemClass}
        role="menuitem"
        type="button"
        onClick={() => onDelete(project)}
      >
        <TrashMenuIcon />
        {t("common.delete")}
      </button>
    </div>
  );
}

function EditIcon() {
  return (
    <span className={`${menuIconClass} text-[19px] leading-none`} aria-hidden="true">
      <Icon name="edit" size="19px" />
    </span>
  );
}

export function ArchiveIcon() {
  return (
    <span className={`${menuIconClass} text-[19px] leading-none`} aria-hidden="true">
      <Icon name="archived" size="19px" />
    </span>
  );
}
