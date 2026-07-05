import { useTranslation } from "react-i18next";

import type { Project } from "../api";
import { modalCancelButtonClass } from "../ThreadActionsMenu";

export function ArchiveProjectModal({
  project,
  error,
  disabled,
  onCancel,
  onArchive,
}: {
  project: Project;
  error: string;
  disabled: boolean;
  onCancel(): void;
  onArchive(): void;
}) {
  const { t } = useTranslation();
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-[rgba(0,0,0,0.5)] px-4 backdrop-blur-[2px]">
      <section
        aria-label={t("projects.archiveModal.label")}
        className="w-full max-w-[460px] rounded-[10px] border border-[#55524b] bg-[#383834] p-6 shadow-[0_24px_60px_rgba(0,0,0,0.45)]"
        role="dialog"
      >
        <h2 className="font-sans text-[22px] font-semibold text-[#f4f0e8]">{t("projects.archiveModal.title")}</h2>
        <p className="mt-3 text-sm leading-6 text-[#d5d2c9]">
          {t("projects.archiveModal.confirm", { name: project.name })}
        </p>
        {error !== "" && <p className="mt-3 text-sm text-[#d98278]">{error}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <button className={modalCancelButtonClass} type="button" onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button
            className="h-8 rounded-md bg-white px-3.5 text-sm font-medium text-[#1d1d1b] transition-colors hover:bg-[#ece9e2] disabled:opacity-50"
            type="button"
            disabled={disabled}
            onClick={onArchive}
          >
            {t("projects.archiveModal.archive")}
          </button>
        </div>
      </section>
    </div>
  );
}
