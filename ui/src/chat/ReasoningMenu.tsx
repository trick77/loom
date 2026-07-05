import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { menuItemClass } from "../ThreadActionsMenu";
import { Icon } from "./Icon";
import { REASONING_OPTIONS, type ReasoningEffort } from "./reasoning";
import { useMenuPlacement } from "./useMenuPlacement";

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
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const { menuRef, verticalClass } = useMenuPlacement();
  const activeLabel = t(`composer.reasoning.${value}`);

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
        aria-label={t("composer.reasoning.ariaLevel", { level: activeLabel })}
        onClick={() => setOpen((current) => !current)}
        className="ml-1 flex h-7 items-center gap-1 rounded-md pl-1 pr-2 text-[13px] leading-none text-[#aaa79e] transition-colors hover:bg-[#3a3a37] hover:text-[#f3f0e8]"
      >
        <span>{activeLabel}</span>
        {/* Border-drawn caret matching the reasoning-title chevron (ui-thinking-chevron):
            a bordered square with no font whitespace, so it centers cleanly on the
            text. Points down when closed, flips up when open. */}
        <span
          aria-hidden
          className={`ml-0.5 inline-block border-solid border-[#aaa79e] border-b-[1.5px] border-r-[1.5px] p-[0.15rem] transition-transform ${
            open ? "translate-y-px rotate-[-135deg]" : "-translate-y-px rotate-45"
          }`}
        />
      </button>
      {open && (
        <div
          ref={menuRef}
          aria-label={t("composer.reasoning.ariaEffort")}
          role="menu"
          // Opens downward when there's room, flipping up only when the composer is
          // docked at the bottom (useMenuPlacement measures the live space).
          className={`absolute right-0 z-30 ${verticalClass} w-[288px] overflow-hidden rounded-[12px] border border-[#454540] bg-[#363632] py-1.5 shadow-[0_18px_32px_rgba(0,0,0,0.38)]`}
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
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    <span className="text-[13px] font-medium text-[#f3f0e8]">{t(`composer.reasoning.${option.value}`)}</span>
                    {option.default === true && (
                      <span className="rounded-[5px] bg-[#4a4741] px-1.5 py-px text-[10px] font-medium uppercase tracking-wide text-[#d8d4ca]">
                        {t("composer.reasoning.default")}
                      </span>
                    )}
                  </span>
                  <span className="mt-0.5 block text-[12px] leading-snug text-[#aaa79e]">
                    {t(`composer.reasoning.${option.value}Desc`)}
                  </span>
                </span>
                {/* Blue right-side checkmark for the active level, matching the
                    language switcher in UserMenu. */}
                {selected && <Icon name="check" size="17px" className="ml-2 mt-0.5 shrink-0 text-[#5599e7]" />}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
