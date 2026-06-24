import { useEffect, useRef, useState } from "react";

import { Icon } from "./chat/Icon";

// Crayon and arrow glyphs are inlined (rather than the icon font) to match
// claude.ai's project-memory composer 1:1.
const CRAYON_PATH =
  "M9.728 2.88a1.5 1.5 0 0 1 1.946-.847l2.792 1.1a1.5 1.5 0 0 1 .845 1.945l-3.92 9.953a1.5 1.5 0 0 1-.452.615l-.088.066-3.143 2.186a.75.75 0 0 1-1.135-.362l-.026-.095-.81-3.742a1.5 1.5 0 0 1 .071-.867zm-2.99 10.319a.5.5 0 0 0-.023.288l.73 3.376 2.835-1.971.058-.047a.5.5 0 0 0 .122-.18l2.637-6.698-3.721-1.466zm4.57-10.236a.5.5 0 0 0-.65.283L9.743 5.57l3.722 1.467.917-2.327a.5.5 0 0 0-.283-.648z";
const ARROW_PATH =
  "M224.49,136.49l-72,72a12,12,0,0,1-17-17L187,140H40a12,12,0,0,1,0-24H187L135.51,64.48a12,12,0,0,1,17-17l72,72A12,12,0,0,1,224.49,136.49Z";

/**
 * MemoryComposer is the crayon affordance in the lower-left of a memory panel. It
 * replicates claude.ai's project-memory composer: a 56px circle that, on click,
 * animates its width out into a full pill-shaped text input (and collapses back to
 * the crayon on Escape / outside click). There is no X.
 *
 * Geometry/animation measured from claude.ai: 56px tall, rounded-full throughout,
 * width morphs over 0.3s cubic-bezier(0.165, 0.84, 0.44, 1).
 *
 * The parent must be `position: relative`; pass a ref wrapping both this and the
 * panel to `useDismissOnOutside`.
 */
export function MemoryComposer({
  open,
  onOpen,
  onClose,
  pending,
  error,
  placeholder = "Tell me what to remember or forget…",
  onSubmit,
}: {
  open: boolean;
  onOpen: () => void;
  onClose: () => void;
  pending: boolean;
  error?: string;
  placeholder?: string;
  onSubmit: (instruction: string) => void;
}) {
  const [draft, setDraft] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) {
      inputRef.current?.focus();
    } else {
      setDraft("");
    }
  }, [open]);

  const canSend = draft.trim() !== "" && !pending;

  function submit() {
    if (!canSend) return;
    onSubmit(draft.trim());
  }

  return (
    <>
      {open && error ? (
        <p
          className="absolute bottom-[4.75rem] left-5 z-20 text-xs text-[#d98278]"
          role="alert"
          aria-live="polite"
          data-testid="memory-edit-error"
        >
          {error}
        </p>
      ) : null}

      <div
        data-testid="memory-edit-composer"
        className={`group absolute bottom-4 left-4 z-20 flex h-14 items-center overflow-hidden rounded-full border border-[#e2e1da]/15 bg-[#2c2c2a]/70 shadow-lg backdrop-blur-sm transition-[width] duration-300 ease-[cubic-bezier(0.165,0.84,0.44,1)] ${
          open ? "w-[calc(100%-2rem)]" : "w-14"
        }`}
      >
        {/* Collapsed: the crayon. Fades out as the input fades in. */}
        <button
          type="button"
          data-testid="memory-edit-button"
          aria-label="Edit memories"
          aria-expanded={open}
          aria-hidden={open}
          tabIndex={open ? -1 : 0}
          onClick={onOpen}
          className={`absolute inset-y-0 left-0 grid w-14 place-items-center text-[#b7b5ad] transition-opacity duration-150 ${
            open ? "pointer-events-none opacity-0" : "opacity-100"
          }`}
        >
          <svg
            width="24"
            height="24"
            viewBox="0 0 20 20"
            fill="currentColor"
            aria-hidden="true"
            className="transition-transform ease-in-out group-hover:rotate-[10deg]"
          >
            <path d={CRAYON_PATH} />
          </svg>
        </button>

        {/* Expanded: the text input. */}
        <div
          className={`flex w-full items-center gap-2 pl-6 pr-2.5 transition-opacity duration-200 ${
            open ? "opacity-100 delay-100" : "pointer-events-none opacity-0"
          }`}
        >
          <input
            ref={inputRef}
            type="text"
            data-testid="memory-edit-input"
            tabIndex={open ? 0 : -1}
            className="ui-composer-text w-full bg-transparent text-[#f3f0e8] outline-none placeholder:text-[#8f8d85]"
            placeholder={placeholder}
            value={draft}
            disabled={pending}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                submit();
              } else if (event.key === "Escape") {
                event.preventDefault();
                onClose();
              }
            }}
          />
          <button
            type="button"
            data-testid="memory-edit-submit"
            aria-label="Apply memory edit"
            disabled={!canSend}
            onClick={submit}
            className="grid h-9 w-9 flex-none place-items-center rounded-full bg-accent text-[#eeeae2] transition-colors hover:bg-accent-strong disabled:cursor-not-allowed disabled:bg-accent disabled:opacity-45"
          >
            {pending ? (
              <Icon name="spinner" size="18px" className="animate-spin" />
            ) : (
              <svg width="18" height="18" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true">
                <path d={ARROW_PATH} />
              </svg>
            )}
          </button>
        </div>
      </div>
    </>
  );
}

/**
 * useDismissOnOutside collapses an open composer when the user clicks anywhere
 * outside `ref` or presses Escape. Pass the wrapper that contains BOTH the composer
 * and the panel so clicking either does not self-dismiss.
 */
export function useDismissOnOutside(
  open: boolean,
  ref: { current: HTMLElement | null },
  onClose: () => void,
) {
  useEffect(() => {
    if (!open) return;
    function onPointerDown(event: MouseEvent) {
      if (ref.current && !ref.current.contains(event.target as Node)) onClose();
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open, ref, onClose]);
}
