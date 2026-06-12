import { useCallback, useLayoutEffect, useRef } from "react";

import { Icon } from "./Icon";

export function Composer({
  variant,
  draft,
  isSending,
  placeholder,
  autoFocus = false,
  onDraftChange,
  onSend,
  onStop,
}: {
  variant: "start" | "chat";
  draft: string;
  isSending: boolean;
  placeholder: string;
  autoFocus?: boolean;
  onDraftChange(value: string): void;
  onSend(): void;
  onStop(): void;
}) {
  // Base (empty) height per variant, preserved as the textarea's min-height so
  // the composer keeps its current look before any auto-grow kicks in.
  const textareaMinH = variant === "start" ? "min-h-[76px]" : "min-h-[56px]";
  const sendIconClass = variant === "chat" ? "h-4 w-4 -translate-y-px" : "h-4 w-4";
  const padX = "px-6";
  const canSend = !isSending && draft.trim() !== "";
  const actionButtonClass = isSending
    ? "bg-[#3a3a37] hover:bg-[#4b4a46]"
    : "bg-accent hover:bg-accent-strong disabled:bg-accent";
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  // Auto-grow: measure content height and apply it inline. The CSS max-height
  // caps the box; once content exceeds it, overflow-y-auto shows the scrollbar.
  // Direction of growth follows layout anchoring (the chat dock is sticky-bottom
  // -> grows upward; the start composer is top-anchored -> grows downward).
  const autoGrow = useCallback(() => {
    const el = textareaRef.current;
    if (el === null) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, []);
  // Re-measure on every draft change (typing, and reset to base after send).
  useLayoutEffect(autoGrow, [autoGrow, draft]);
  useLayoutEffect(() => {
    if (autoFocus) textareaRef.current?.focus();
  }, [autoFocus]);
  // Re-measure when the textarea's width changes (window resize, breakpoint,
  // rotation) - a different width re-wraps the text and changes the needed
  // height. Guard on width only: autoGrow mutates the element's height, so
  // reacting to height changes too would re-trigger the observer.
  useLayoutEffect(() => {
    const el = textareaRef.current;
    if (el === null) return;
    let lastWidth = el.clientWidth;
    const observer = new ResizeObserver(() => {
      if (el.clientWidth === lastWidth) return;
      lastWidth = el.clientWidth;
      autoGrow();
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [autoGrow]);
  return (
    <form
      className="ui-composer relative flex flex-col rounded-[20px] border border-[#4b4a46] bg-[#2a2a28] shadow-[0_14px_24px_rgba(0,0,0,0.22)]"
      onSubmit={(event) => {
        event.preventDefault();
        if (isSending) {
          onStop();
          return;
        }
        onSend();
      }}
    >
      <textarea
        ref={textareaRef}
        className={`ui-composer-text ui-sidebar-scroll ${textareaMinH} w-full resize-none overflow-y-auto bg-transparent ${padX} pb-3 pt-5 text-[#f3f0e8] outline-none placeholder:text-[#aaa79e] max-h-[150px] md:max-h-[264px]`}
        placeholder={placeholder}
        value={draft}
        onChange={(event) => onDraftChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter" && !event.shiftKey) {
            event.preventDefault();
            if (!isSending) onSend();
          }
        }}
      />
      <div className={`flex h-11 flex-none items-center justify-between ${padX} text-[#d8d4ca]`}>
        <button className="leading-none" type="button" aria-label="Add attachment">
          <Icon name="plus" size="24px" />
        </button>
        <div className="ui-meta-text flex items-center text-[#d8d4ca]">
          <button
            className={`ui-composer-send grid h-7 w-7 place-items-center rounded-md text-[#eeeae2] transition-colors disabled:cursor-not-allowed disabled:opacity-45 ${actionButtonClass}`}
            disabled={!isSending && !canSend}
            type="submit"
            aria-label={isSending ? "Stop response" : "Send message"}
          >
            {isSending ? (
              <svg className={sendIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="currentColor">
                <rect x="5.5" y="5.5" width="13" height="13" rx="2" />
              </svg>
            ) : (
              <svg className={sendIconClass} viewBox="0 0 24 24" aria-hidden="true" fill="none">
                <path
                  d="M12 19V5M6.5 10.5 12 5l5.5 5.5"
                  stroke="currentColor"
                  strokeWidth="2.4"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            )}
          </button>
        </div>
      </div>
    </form>
  );
}
