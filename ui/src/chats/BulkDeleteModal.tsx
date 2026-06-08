import { useEffect } from "react";

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
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onCancel();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onCancel]);

  const label = count === 1 ? "this chat" : `these ${count} chats`;
  return (
    <div
      className="fixed inset-0 z-40 grid place-items-center bg-[rgba(10,10,9,0.62)] pr-4 pl-[378px]"
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
          {count === 1 ? "Delete chat" : `Delete ${count} chats`}
        </h2>
        <div className="mt-3 text-[13px] leading-5 text-[#d8d4ca]">
          Are you sure you want to delete {label}? This cannot be undone.
        </div>
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
            onClick={onConfirm}
            type="button"
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}
