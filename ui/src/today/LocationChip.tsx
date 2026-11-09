import { useCallback, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import type { TodayLocation } from "../api/today";
import { useEscapeKey } from "../chat/useEscapeKey";
import { useOutsideRefPointerDown } from "../chat/useOutsidePointerDown";
import { PinIcon } from "./WeatherIcon";

// LocationChip names the place the weather is for. A click explains where
// loom got it and how to change it.
export function LocationChip({
  location,
  compact = false,
  onOpenMemory,
}: {
  location: TodayLocation;
  compact?: boolean;
  onOpenMemory(): void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const dialogId = useId();
  const close = useCallback(() => setOpen(false), []);
  useOutsideRefPointerDown(open, ref, close);
  useEscapeKey(close, { active: open });
  return (
    <div ref={ref} className="relative">
      <button
        aria-controls={open ? dialogId : undefined}
        aria-expanded={open}
        aria-haspopup="dialog"
        className={`ui-control-text flex items-center gap-1.5 rounded-full text-[13px] transition-colors hover:bg-[#2a2a28] ${
          compact
            ? "h-8 px-2 text-[#97958c]"
            : "h-[30px] border border-[#343432] px-2.5 text-[#d5d2c9]"
        }`}
        onClick={() => setOpen((value) => !value)}
        type="button"
      >
        <PinIcon />
        {location.name}
        {/* The space is for the accessible name; flex drops it on screen. */}
        {!compact && " "}
        {!compact && (
          <span className="text-[#8f8b82]">
            · {t("today.location.fromMemories")}
          </span>
        )}
      </button>
      {open && (
        <div
          aria-label={t("today.location.title")}
          className={`absolute top-[38px] z-10 flex ${
            // The compact chip sits mid-line, on a phone mid-screen: centered
            // under it the popover fits; the header chip sits at the right.
            compact ? "left-1/2 -translate-x-1/2" : "right-0"
          } w-[min(320px,calc(100vw-32px))] flex-col gap-2.5 rounded-2xl border border-[rgba(226,225,218,0.15)] bg-active p-4 text-sm text-[#d5d2c9] shadow-[0_12px_32px_rgba(0,0,0,0.4)]`}
          id={dialogId}
          role="dialog"
        >
          <div className="font-medium text-ink">
            {t("today.location.title")}
          </div>
          <p className="text-[#c7c5bd]">
            {t(
              location.kind === "current"
                ? "today.location.current"
                : "today.location.home",
              { name: location.name },
            )}{" "}
            {t("today.location.change")}
          </p>
          <div>
            <button
              className="ui-control-text h-8 rounded-lg bg-[rgba(255,255,255,0.1)] px-3 text-[13px] text-ink transition-colors hover:bg-[rgba(255,255,255,0.16)]"
              onClick={() => {
                setOpen(false);
                onOpenMemory();
              }}
              type="button"
            >
              {t("today.location.openMemories")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
