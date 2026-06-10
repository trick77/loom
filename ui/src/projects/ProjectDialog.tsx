import { useEffect, useState } from "react";

import type { Project } from "../api";

export function ProjectDialog({
  project,
  error,
  disabled,
  onCancel,
  onSubmit,
}: {
  project: Project | null;
  error: string;
  disabled: boolean;
  onCancel(): void;
  onSubmit(input: { name: string; description: string }): void;
}) {
  const [name, setName] = useState(project?.name ?? "");
  const [description, setDescription] = useState(project?.description ?? "");
  const title = project === null ? "New project" : "Edit details";

  useEffect(() => {
    setName(project?.name ?? "");
    setDescription(project?.description ?? "");
  }, [project]);

  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/60 px-4">
      <form
        aria-label={title}
        className="w-full max-w-[520px] rounded-[10px] border border-[#55524b] bg-[#383834] p-6 shadow-[0_24px_60px_rgba(0,0,0,0.45)]"
        role="dialog"
        onSubmit={(event) => {
          event.preventDefault();
          const trimmed = name.trim();
          if (trimmed !== "") onSubmit({ name: trimmed, description: description.trim() });
        }}
      >
        <div className="flex items-center justify-between gap-4">
          <h2 className="font-sans text-[22px] font-semibold text-[#f4f0e8]">{title}</h2>
          <button
            className="text-2xl leading-none text-[#d5d2c9] hover:text-white"
            type="button"
            aria-label="Close"
            onClick={onCancel}
          >
            x
          </button>
        </div>
        <label className="mt-4 block text-sm text-[#f4f0e8]">
          Name
          <input
            className="mt-2 h-9 w-full rounded-md border border-transparent bg-[#555550] px-3 text-sm text-white outline-none focus:border-[#8d897f]"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </label>
        <label className="mt-4 block text-sm text-[#f4f0e8]">
          Description
          <textarea
            className="mt-2 min-h-[136px] w-full resize-none rounded-md border border-transparent bg-[#555550] px-3 py-3 text-sm text-white outline-none focus:border-[#8d897f]"
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        </label>
        {error !== "" && <p className="mt-3 text-sm text-[#d98278]">{error}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button
            className="rounded-md bg-[#5c5b56] px-3 py-2 text-sm font-medium text-white hover:bg-[#696861]"
            type="button"
            onClick={onCancel}
          >
            Cancel
          </button>
          <button
            className="rounded-md bg-white px-3 py-2 text-sm font-medium text-[#1d1d1b] disabled:opacity-50"
            type="submit"
            disabled={disabled || name.trim() === ""}
          >
            {project === null ? "Create" : "Save"}
          </button>
        </div>
      </form>
    </div>
  );
}
