import type { Document } from "../api";
import { modalCancelButtonClass, modalDangerButtonClass } from "../ThreadActionsMenu";

export function DeleteDocumentModal({
  doc,
  error,
  disabled,
  onCancel,
  onDelete,
}: {
  doc: Document;
  error: string;
  disabled: boolean;
  onCancel(): void;
  onDelete(): void;
}) {
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-[rgba(0,0,0,0.5)] px-4 backdrop-blur-[2px]">
      <section
        aria-label="Remove document"
        className="w-full max-w-[460px] rounded-[10px] border border-[#55524b] bg-[#383834] p-6 shadow-[0_24px_60px_rgba(0,0,0,0.45)]"
        role="dialog"
      >
        <h2 className="font-sans text-[22px] font-semibold text-[#f4f0e8]">Remove document</h2>
        <p className="mt-3 text-sm leading-6 text-[#d5d2c9]">
          Remove {doc.filename}? It will no longer be searchable or citable by threads in this
          project.
        </p>
        {error !== "" && <p className="mt-3 text-sm text-[#d98278]">{error}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <button className={modalCancelButtonClass} type="button" onClick={onCancel}>
            Cancel
          </button>
          <button className={modalDangerButtonClass} type="button" disabled={disabled} onClick={onDelete}>
            Remove
          </button>
        </div>
      </section>
    </div>
  );
}
