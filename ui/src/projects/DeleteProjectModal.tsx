import { useTranslation } from "react-i18next";

import type { Project } from "../api";
import {
  modalCancelButtonClass,
  modalDangerButtonClass,
} from "../ThreadActionsMenu";

export function DeleteProjectModal({
  project,
  error,
  disabled,
  onCancel,
  onDelete,
}: {
  project: Project;
  error: string;
  disabled: boolean;
  onCancel(): void;
  onDelete(): void;
}) {
  const { t } = useTranslation();
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-[rgba(0,0,0,0.5)] px-4 backdrop-blur-[2px]">
      <section
        aria-label={t("projects.deleteModal.label")}
        className="w-full max-w-[460px] rounded-[10px] border border-[#55524b] bg-[#383834] p-6 shadow-[0_24px_60px_rgba(0,0,0,0.45)]"
        role="dialog"
      >
        <h2 className="font-sans text-[22px] font-semibold text-[#f4f0e8]">
          {t("projects.deleteModal.title")}
        </h2>
        <p className="mt-3 text-sm leading-6 text-[#d5d2c9]">
          {t("projects.deleteModal.confirm", { name: project.name })}
        </p>
        {error !== "" && <p className="mt-3 text-sm text-[#d98278]">{error}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <button
            className={modalCancelButtonClass}
            type="button"
            onClick={onCancel}
          >
            {t("common.cancel")}
          </button>
          <button
            className={modalDangerButtonClass}
            type="button"
            disabled={disabled}
            onClick={onDelete}
          >
            {t("common.delete")}
          </button>
        </div>
      </section>
    </div>
  );
}
