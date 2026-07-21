import { type ReactNode, useEffect, useId, useRef } from "react";
import { useTranslation } from "react-i18next";

import {
  modalCancelButtonClass,
  modalDangerButtonClass,
} from "../ThreadActionsMenu";
import { ErrorText } from "./ErrorText";

export function RenameThreadModal({
  title,
  error,
  disabled,
  onTitleChange,
  onCancel,
  onSubmit,
}: {
  title: string;
  error: string;
  disabled: boolean;
  onTitleChange(value: string): void;
  onCancel(): void;
  onSubmit(): void;
}) {
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);
  return (
    <ModalShell title={t("thread.renameTitle")} onCancel={onCancel}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <input
          ref={inputRef}
          aria-label={t("thread.threadTitleLabel")}
          className="ui-control-text mt-3 h-[38px] w-full rounded-lg border border-[#5b5851] bg-[#1f1f1d] px-3 text-[#f3f0e8] outline-none selection:bg-[#6f6250] selection:text-[#fffaf2]"
          value={title}
          onChange={(event) => onTitleChange(event.target.value)}
        />
        {error !== "" && <ErrorText>{error}</ErrorText>}
        <div className="mt-4 flex justify-end gap-2">
          <button
            className="h-8 rounded-md px-3 text-sm text-[#c7c5bd] hover:bg-[#363632]"
            onClick={onCancel}
            type="button"
          >
            {t("common.cancel")}
          </button>
          <button
            className="h-8 rounded-md bg-[#50483d] px-3.5 text-sm font-medium text-[#fffaf2] disabled:opacity-50"
            disabled={disabled || title.trim() === ""}
            type="submit"
          >
            {t("common.save")}
          </button>
        </div>
      </form>
    </ModalShell>
  );
}

export function DeleteThreadModal({
  error,
  disabled,
  onCancel,
  onDelete,
}: {
  error: string;
  disabled: boolean;
  onCancel(): void;
  onDelete(): void;
}) {
  const { t } = useTranslation();
  return (
    <ModalShell title={t("thread.deleteTitle")} onCancel={onCancel}>
      <div className="mt-3 text-sm leading-6 text-[#d8d4ca]">
        {t("thread.deleteConfirm")}
      </div>
      {error !== "" && <ErrorText>{error}</ErrorText>}
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
          onClick={onDelete}
          type="button"
        >
          {t("common.delete")}
        </button>
      </div>
    </ModalShell>
  );
}

export function ModalShell({
  title,
  children,
  onCancel,
}: {
  title: string;
  children: ReactNode;
  onCancel(): void;
}) {
  const titleID = useId();
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
        aria-labelledby={titleID}
        aria-modal="true"
        className="w-full max-w-[390px] rounded-xl border border-[#4b4a46] bg-[#2a2a28] p-[18px] shadow-[0_28px_70px_rgba(0,0,0,0.55)]"
        role="dialog"
      >
        <h2
          id={titleID}
          className="font-sans text-[22px] font-semibold leading-7 text-[#f3f0e8]"
        >
          {title}
        </h2>
        {children}
      </div>
    </div>
  );
}
