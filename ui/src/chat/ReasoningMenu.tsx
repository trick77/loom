import { useEffect, useRef, useState } from "react";

import { menuItemClass } from "../ThreadActionsMenu";
import { Icon } from "./Icon";
import { REASONING_OPTIONS, reasoningLabel, type ReasoningEffort } from "./reasoning";

// ReasoningMenu — the composer's reasoning-effort selector. A compact trigger
// showing the active level opens a menu (flipping up when there's no room below,
// which is the norm at the bottom of the composer) listing each level with a
// description. There is no model picker beside it: Loom serves one model, shown as
// a static label (see Composer). Self-contained open/close so both the start and
// thread composers can drop it in without lifting menu state.
export function ReasoningMenu({
  value,
  onChange,
}: {
  value: ReasoningEffort;
  onChange(value: ReasoningEffort): void;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    // Pointerdown (not click) so a press anywhere outside dismisses before it can
    // steal focus or trigger another control.
    function onPointerDown(event: PointerEvent) {
      if (rootRef.current !== null && !rootRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    window.addEventListener("keydown", onKey);
    window.addEventListener("pointerdown", onPointerDown, true);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("pointerdown", onPointerDown, true);
    };
  }, [open]);

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`Reasoning: ${reasoningLabel(value)}`}
        onClick={() => setOpen((current) => !current)}
        className="flex h-7 items-center gap-1 rounded-md px-2 text-[13px] leading-none text-[#aaa79e] transition-colors hover:bg-[#3a3a37] hover:text-[#f3f0e8]"
      >
        <Icon name="sliders" size="15px" />
        <span>{reasoningLabel(value)}</span>
        {/* Border-drawn caret matching the reasoning-title chevron (ui-thinking-chevron):
            a bordered square with no font whitespace, so it centers cleanly on the
            text. Points down when closed, flips up when open. */}
        <span
          aria-hidden
          className={`ml-0.5 inline-block border-solid border-b-[1.5px] border-r-[1.5px] p-[0.15rem] transition-transform ${
            open ? "translate-y-px rotate-[-135deg]" : "-translate-y-px rotate-45"
          }`}
        />
      </button>
      {open && (
        <div
          aria-label="Reasoning effort"
          role="menu"
          // Always opens upward, mirroring the composer's slash-command popover
          // (which also uses bottom-full): the composer sits at the bottom of the
          // thread dock, so dropping down would clip against the viewport edge.
          className="absolute bottom-full right-0 z-30 mb-2 w-[288px] overflow-hidden rounded-[12px] border border-[#454540] bg-[#363632] py-1.5 shadow-[0_18px_32px_rgba(0,0,0,0.38)]"
        >
          {REASONING_OPTIONS.map((option) => {
            const selected = option.value === value;
            return (
              <button
                key={option.value}
                type="button"
                role="menuitemradio"
                aria-checked={selected}
                onClick={() => {
                  onChange(option.value);
                  setOpen(false);
                }}
                className={`${menuItemClass} py-1.5`}
              >
                <span className="mt-0.5 grid h-[16px] w-[16px] shrink-0 place-items-center text-[#f3f0e8]">
                  {selected && <Icon name="check" size="15px" />}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    <span className="text-[13px] font-medium text-[#f3f0e8]">{option.label}</span>
                    {option.badge !== undefined && (
                      <span className="rounded-[5px] bg-[#4a4741] px-1.5 py-px text-[10px] font-medium uppercase tracking-wide text-[#d8d4ca]">
                        {option.badge}
                      </span>
                    )}
                  </span>
                  <span className="mt-0.5 block text-[12px] leading-snug text-[#aaa79e]">{option.description}</span>
                </span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
