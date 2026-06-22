import type { Project } from "../api";

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
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 px-4">
      <section
        aria-label="Delete project"
        className="w-full max-w-[460px] rounded-[10px] border border-[#55524b] bg-[#383834] p-6 shadow-[0_24px_60px_rgba(0,0,0,0.45)]"
        role="dialog"
      >
        <h2 className="font-sans text-[22px] font-semibold text-[#f4f0e8]">Delete project</h2>
        <p className="mt-3 text-sm leading-6 text-[#d5d2c9]">
          Delete {project.name}? This permanently deletes the project, its threads, and generated
          artifacts for those threads.
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
            className="rounded-md bg-[#c9534b] px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
            type="button"
            disabled={disabled}
            onClick={onDelete}
          >
            Delete
          </button>
        </div>
      </section>
    </div>
  );
}
