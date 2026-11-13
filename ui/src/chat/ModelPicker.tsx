import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { ChatSlot } from "../api/model";
import { menuItemClass } from "../ThreadActionsMenu";
import { Icon } from "./Icon";

// The composer's model label. With one configured slot it is the plain name;
// with a deep slot it opens a menu to switch the thread between the fast
// default and the model that thinks longer. The client only ever names a slot
// key; the backend maps it to the model.
export function ModelPicker({
  slots,
  value,
  fallbackName,
  onChange,
}: {
  slots: ChatSlot[];
  value: string;
  fallbackName?: string;
  onChange?(slot: string): void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const current = slots.find((slot) => slot.key === value) ?? slots[0];
  const name = current?.displayName ?? fallbackName;

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", onPointer);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onPointer);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  if (slots.length < 2 || onChange === undefined) {
    return (
      <span className="mr-2 hidden select-none text-[13px] leading-none text-[#aaa79e] sm:inline">
        {name}
      </span>
    );
  }
  return (
    <div ref={rootRef} className="relative mr-1 hidden sm:block">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t("composer.chooseModel", { name })}
        className="flex items-center gap-1 rounded-md px-1.5 py-1 text-[13px] leading-none text-[#aaa79e] transition-colors hover:bg-[#3a3a37] hover:text-[#eeeae2]"
        onClick={() => setOpen((was) => !was)}
      >
        {name}
        <Icon name="chevronDown" size="14px" />
      </button>
      {open && (
        <div
          role="menu"
          aria-label={t("composer.models")}
          className="ui-sidebar-text absolute bottom-full right-0 z-30 mb-2 w-[264px] overflow-hidden rounded-[10px] border border-[#454540] bg-[#363632] py-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)]"
        >
          {slots.map((slot) => (
            <button
              key={slot.key}
              type="button"
              role="menuitemradio"
              aria-checked={slot.key === current?.key}
              className={`${menuItemClass} py-1.5 text-[#f3f0e8]`}
              onClick={() => {
                setOpen(false);
                if (slot.key !== current?.key) onChange(slot.key);
              }}
            >
              <span className="min-w-0 flex-1 text-left">
                <span className="block truncate">{slot.displayName}</span>
                <span className="block text-[12px] leading-snug text-[#aaa79e]">
                  {t(slotDescriptionKey(slot.key))}
                </span>
              </span>
              {slot.key === current?.key && (
                <Icon
                  name="check"
                  size="17px"
                  className="ml-2 mt-0.5 shrink-0 text-[#5599e7]"
                />
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function slotDescriptionKey(key: string): string {
  return key === "deep" ? "composer.slotDeep" : "composer.slotStandard";
}
