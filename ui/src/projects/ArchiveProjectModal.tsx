import type { Project } from "../api";

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
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 px-4">
      <section
        aria-label="Archive project"
        className="w-full max-w-[460px] rounded-[10px] border border-[#55524b] bg-[#383834] p-6 shadow-[0_24px_60px_rgba(0,0,0,0.45)]"
        role="dialog"
      >
        <h2 className="font-sans text-[22px] font-semibold text-[#f4f0e8]">Archive project</h2>
        <p className="mt-3 text-sm leading-6 text-[#d5d2c9]">
          Are you sure you want to archive {project.name}? Its threads disappear from your recents but
          stay searchable, and you can unarchive it anytime.
        </p>
        {error !== "" && <p className="mt-3 text-sm text-[#d98278]">{error}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <button
            className="rounded-md bg-[#5c5b56] px-3 py-2 text-sm font-medium text-white hover:bg-[#696861]"
            type="button"
            onClick={onCancel}
          >
            Cancel
          </button>
          <button
            className="rounded-md bg-white px-3 py-2 text-sm font-medium text-[#1d1d1b] disabled:opacity-50"
            type="button"
            disabled={disabled}
            onClick={onArchive}
          >
            Archive
          </button>
        </div>
      </section>
    </div>
  );
}
