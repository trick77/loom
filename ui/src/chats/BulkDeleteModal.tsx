import { useEffect } from "react";
import { useTranslation } from "react-i18next";

import {
  modalCancelButtonClass,
  modalDangerButtonClass,
} from "../ThreadActionsMenu";

export function BulkDeleteModal({
  count,
  disabled,
  onCancel,
  onConfirm,
}: {
  count: number;
  disabled: boolean;
  onCancel(): void;
  onConfirm(): void;
}) {
  const { t } = useTranslation();

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onCancel();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onCancel]);

  return (
    <div
      className="fixed inset-0 z-40 grid place-items-center bg-[rgba(0,0,0,0.5)] px-4 backdrop-blur-[2px] md:pr-4 md:pl-[378px]"
      onClick={(event) => {
        if (event.target === event.currentTarget) onCancel();
      }}
    >
      <div
        aria-modal="true"
        role="dialog"
        className="w-full max-w-[390px] rounded-xl border border-[#4b4a46] bg-[#2a2a28] p-[18px] shadow-[0_28px_70px_rgba(0,0,0,0.55)]"
      >
        <h2 className="font-sans text-[22px] font-semibold leading-7 text-[#f3f0e8]">
          {t("chats.bulkDelete.title", { count })}
        </h2>
        <div className="mt-3 text-[13px] leading-5 text-[#d8d4ca]">
          {t("chats.bulkDelete.confirm", { count })}
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <button
            autoFocus
            className={modalCancelButtonClass}
            onClick={onCancel}
            type="button"
          >
            {t("common.cancel")}
          </button>
          <button
            className={modalDangerButtonClass}
            disabled={disabled}
            onClick={onConfirm}
            type="button"
          >
            {t("common.delete")}
          </button>
        </div>
      </div>
    </div>
  );
}
