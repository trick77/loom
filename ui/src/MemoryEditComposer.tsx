import { useEffect, useRef, useState } from "react";

import { Icon } from "./chat/Icon";

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
      <div className="flex items-end gap-2 rounded-2xl border border-[#454540] bg-[#2a2a27] px-4 py-2.5 shadow-lg">
        <textarea
          ref={textareaRef}
          rows={1}
          data-testid="memory-edit-input"
          className="ui-sidebar-scroll max-h-[120px] w-full resize-none overflow-y-auto bg-transparent text-sm leading-5 text-[#f3f0e8] outline-none placeholder:text-[#aaa79e]"
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
