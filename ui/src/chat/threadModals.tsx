import { type ReactNode, useEffect, useId, useRef } from "react";

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
  const inputRef = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);
  return (
    <ModalShell title="Rename chat" onCancel={onCancel}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit();
        }}
      >
        <input
          ref={inputRef}
          aria-label="Chat title"
          className="ui-control-text mt-3 h-[38px] w-full rounded-lg border border-[#5b5851] bg-[#1f1f1d] px-3 text-[#f3f0e8] outline-none selection:bg-[#6f6250] selection:text-[#fffaf2]"
          value={title}
          onChange={(event) => onTitleChange(event.target.value)}
        />
        {error !== "" && <ErrorText>{error}</ErrorText>}
        <div className="mt-4 flex justify-end gap-2">
          <button className="h-8 rounded-md px-3 text-[#c7c5bd] hover:bg-[#363632]" onClick={onCancel} type="button">
            Cancel
          </button>
          <button
            className="h-8 rounded-md bg-[#50483d] px-3.5 font-medium text-[#fffaf2] disabled:opacity-50"
            disabled={disabled || title.trim() === ""}
            type="submit"
          >
            Save
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
  return (
    <ModalShell title="Delete chat" onCancel={onCancel}>
      <div className="mt-3 text-[13px] leading-5 text-[#d8d4ca]">Are you sure you want to delete this chat?</div>
      {error !== "" && <ErrorText>{error}</ErrorText>}
      <div className="mt-4 flex justify-end gap-2">
        <button
          autoFocus
          className="h-8 rounded-md px-3 text-[#c7c5bd] hover:bg-[#363632]"
          onClick={onCancel}
          type="button"
        >
          Cancel
        </button>
        <button
          className="h-8 rounded-md bg-[#b85c52] px-3.5 font-medium text-[#fffaf2] disabled:opacity-50"
          disabled={disabled}
          onClick={onDelete}
          type="button"
        >
          Delete
        </button>
      </div>
    </ModalShell>
  );
}

function ModalShell({
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
      className="fixed inset-0 z-40 grid place-items-center bg-[rgba(10,10,9,0.62)] px-4 md:pr-4 md:pl-[378px]"
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
        <h2 id={titleID} className="font-sans text-[22px] font-semibold leading-7 text-[#f3f0e8]">
          {title}
        </h2>
        {children}
      </div>
    </div>
  );
}
