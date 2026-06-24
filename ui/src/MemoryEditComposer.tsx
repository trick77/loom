import { useEffect, useRef, useState } from "react";

import { Icon } from "./chat/Icon";

/**
 * MemoryEditButton is the circular crayon affordance that floats half-outside the
 * left edge of a memory panel (see the reference design). It only ever shows the
 * crayon — the composer is dismissed with Escape or an outside click, never an X.
 * Its parent must be `position: relative`.
 */
export function MemoryEditButton({ open, onClick }: { open: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      data-testid="memory-edit-button"
      aria-label="Edit memories"
      aria-expanded={open}
      onClick={onClick}
      className="absolute left-0 top-1/2 z-20 grid h-12 w-12 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full border border-[#3f3f3a] bg-[#2a2a27] text-[#d5d2c9] shadow-lg transition-colors hover:bg-[#33332f]"
    >
      <Icon name="edit" size="22px" />
    </button>
  );
}

/**
 * useDismissOnOutside closes an open composer when the user clicks anywhere
 * outside `ref` or presses Escape. Pass the wrapper that contains BOTH the crayon
 * button and the composer so clicking either does not self-dismiss.
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

/**
 * MemoryEditComposer is the small inline composer revealed by the crayon button
 * on a memory panel. The user types a natural-language instruction — "Remember I
 * live in Zurich", "Forget my baseball career" — which the backend applies to the
 * memory in place. It is intentionally lightweight (no attachments): a growing
 * input and a submit arrow. Submit on Enter, dismiss on Escape.
 */
export function MemoryEditComposer({
  placeholder = "Tell me what to remember or forget…",
  pending,
  error,
  onSubmit,
  onClose,
}: {
  placeholder?: string;
  pending: boolean;
  error?: string;
  onSubmit: (instruction: string) => void;
  onClose: () => void;
}) {
  const [draft, setDraft] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Focus on open and auto-grow up to a small cap.
  useEffect(() => {
    textareaRef.current?.focus();
  }, []);
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 120)}px`;
  }, [draft]);

  const canSend = draft.trim() !== "" && !pending;

  function submit() {
    if (!canSend) return;
    onSubmit(draft.trim());
  }

  return (
    <div className="flex flex-col gap-1" data-testid="memory-edit-composer">
      <div className="flex items-center gap-2 rounded-2xl border border-[#454540] bg-[#2a2a27] px-4 py-3 shadow-lg">
        <textarea
          ref={textareaRef}
          rows={1}
          data-testid="memory-edit-input"
          className="ui-composer-text ui-sidebar-scroll block max-h-[120px] w-full resize-none overflow-y-auto bg-transparent text-[#f3f0e8] outline-none placeholder:text-[#aaa79e]"
          placeholder={placeholder}
          value={draft}
          disabled={pending}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && !event.shiftKey) {
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
          className="grid h-8 w-8 flex-none place-items-center rounded-full bg-[#d97757] text-[#1a1a18] transition-colors enabled:hover:bg-[#e08a6e] disabled:bg-[#3f3f3a] disabled:text-[#807d74]"
        >
          {pending ? (
            <Icon name="spinner" size="16px" className="animate-spin" />
          ) : (
            <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" fill="none">
              <path
                d="M5 12h14M13 6l6 6-6 6"
                stroke="currentColor"
                strokeWidth="2.4"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          )}
        </button>
      </div>
      {error ? (
        <p
          className="px-1 text-xs text-[#d98278]"
          data-testid="memory-edit-error"
          role="alert"
          aria-live="polite"
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}
