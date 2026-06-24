import { useEffect, useRef, useState } from "react";

import { ErrorText } from "../chat/ErrorText";
import { ModalShell } from "../chat/threadModals";

// splitFilename separates a filename into an editable stem and a fixed extension
// (including the dot). A leading dot (dotfiles) or no dot yields an empty
// extension, so the whole name stays editable.
export function splitFilename(filename: string): { stem: string; extension: string } {
  const dot = filename.lastIndexOf(".");
  if (dot <= 0) return { stem: filename, extension: "" };
  return { stem: filename.slice(0, dot), extension: filename.slice(dot) };
}

export function RenameArtifactModal({
  filename,
  error,
  disabled,
  onCancel,
  onSubmit,
}: {
  filename: string;
  error: string;
  disabled: boolean;
  onCancel(): void;
  onSubmit(displayFilename: string): void;
}) {
  const { stem, extension } = splitFilename(filename);
  const [value, setValue] = useState(stem);
  const inputRef = useRef<HTMLInputElement | null>(null);
  // Pre-select the stem on open so typing replaces the name immediately (the
  // extension stays fixed and is shown as a non-editable suffix).
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);

  const trimmed = value.trim();
  return (
    <ModalShell title="Rename artifact" onCancel={onCancel}>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          if (trimmed === "") return;
          onSubmit(trimmed + extension);
        }}
      >
        <div className="mt-3 flex items-center rounded-lg border border-[#5b5851] bg-[#1f1f1d] pr-3 focus-within:border-[#807d74]">
          <input
            ref={inputRef}
            aria-label="Artifact filename"
            className="ui-control-text h-[38px] min-w-0 flex-1 bg-transparent px-3 text-[#f3f0e8] outline-none selection:bg-[#6f6250] selection:text-[#fffaf2]"
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
          {extension !== "" && (
            <span className="ui-control-text shrink-0 text-[#8a887f]">{extension}</span>
          )}
        </div>
        {error !== "" && <ErrorText>{error}</ErrorText>}
        <div className="mt-4 flex justify-end gap-2">
          <button className="h-8 rounded-md px-3 text-sm text-[#c7c5bd] hover:bg-[#363632]" onClick={onCancel} type="button">
            Cancel
          </button>
          <button
            className="h-8 rounded-md bg-[#50483d] px-3.5 text-sm font-medium text-[#fffaf2] disabled:opacity-50"
            disabled={disabled || trimmed === ""}
            type="submit"
          >
            Save
          </button>
        </div>
      </form>
    </ModalShell>
  );
}

export function DeleteArtifactModal({
  filename,
  error,
  disabled,
  onCancel,
  onDelete,
}: {
  filename: string;
  error: string;
  disabled: boolean;
  onCancel(): void;
  onDelete(): void;
}) {
  return (
    <ModalShell title="Delete artifact" onCancel={onCancel}>
      <div className="mt-3 text-sm leading-6 text-[#d8d4ca]">
        Delete <span className="font-medium text-[#f3f0e8]">{filename}</span>? It will be removed from
        your artifacts. Chats that used it will show the file as deleted.
      </div>
      {error !== "" && <ErrorText>{error}</ErrorText>}
      <div className="mt-4 flex justify-end gap-2">
        <button
          autoFocus
          className="h-8 rounded-md px-3 text-sm text-[#c7c5bd] hover:bg-[#363632]"
          onClick={onCancel}
          type="button"
        >
          Cancel
        </button>
        <button
          className="h-8 rounded-md bg-[#b85c52] px-3.5 text-sm font-medium text-[#fffaf2] disabled:opacity-50"
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
