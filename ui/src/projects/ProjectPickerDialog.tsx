import type { Project, Thread } from "../api";

export function ProjectPickerDialog({
  threads,
  projects,
  error,
  disabled,
  onCancel,
  onSelect,
}: {
  threads: Thread[];
  projects: Project[];
  error: string;
  disabled: boolean;
  onCancel(): void;
  onSelect(project: Project): void;
}) {
  const title = threads.length === 1 ? "Add to project" : "Move to project";
  const subtitle = threads.length === 1 ? threads[0]?.title ?? "" : `${threads.length} chats selected`;

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 px-4">
      <section
        aria-label={title}
        className="w-full max-w-[460px] rounded-[10px] border border-[#55524b] bg-[#383834] p-5 shadow-[0_24px_60px_rgba(0,0,0,0.45)]"
        role="dialog"
      >
        <div className="flex items-center justify-between gap-4">
          <h2 className="font-sans text-[20px] font-semibold text-[#f4f0e8]">{title}</h2>
          <button
            className="text-2xl leading-none text-[#d5d2c9] hover:text-white"
            type="button"
            aria-label="Close"
            onClick={onCancel}
          >
            x
          </button>
        </div>
        <p className="mt-2 truncate text-sm text-[#aaa79e]">{subtitle}</p>
        <div className="mt-4 max-h-[260px] overflow-y-auto">
          {projects.length === 0 ? (
            <p className="py-6 text-center text-sm text-[#807d74]">No projects yet.</p>
          ) : (
            projects.map((project) => (
              <button
                key={project.id}
                aria-label={project.name}
                className="flex w-full flex-col rounded-md px-3 py-2 text-left hover:bg-[#2a2a28] disabled:opacity-50"
                type="button"
                disabled={disabled}
                onClick={() => onSelect(project)}
              >
                <span className="text-sm text-[#f4f0e8]">{project.name}</span>
                {project.description !== "" && (
                  <span className="mt-1 truncate text-xs text-[#aaa79e]">{project.description}</span>
                )}
              </button>
            ))
          )}
        </div>
        {error !== "" && <p className="mt-3 text-sm text-[#d98278]">{error}</p>}
      </section>
    </div>
  );
}
