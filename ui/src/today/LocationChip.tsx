import { useCallback, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { dropLocated, type Attribution } from "../api";
import type { TodayLocation } from "../api/today";
import { useEscapeKey } from "../chat/useEscapeKey";
import { useOutsideRefPointerDown } from "../chat/useOutsidePointerDown";
import { LocateButton } from "./LocationPrompt";
import { canGeolocate, useLocate, type LocateFailure } from "./useLocate";
import { PinIcon } from "./WeatherIcon";

const failureText: Record<LocateFailure, string> = {
  denied: "today.location.chipDenied",
  failed: "today.location.chipFailed",
  noTown: "today.location.chipNoTown",
  retry: "today.location.chipRetry",
};

// LocationChip names the place the weather is for. A click says where loom
// got it; the browser's position can replace a place from memory (stale or
// wrong, maybe) without a chat, and be dropped again.
export function LocationChip({
  location,
  credit = null,
  canLocate = false,
  compact = false,
  onChanged,
}: {
  location: TodayLocation;
  credit?: Attribution | null;
  canLocate?: boolean;
  compact?: boolean;
  /** The place changed: reload the section. */
  onChanged(): void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const dialogId = useId();
  const close = useCallback(() => setOpen(false), []);
  useOutsideRefPointerDown(open, ref, close);
  useEscapeKey(close, { active: open });
  const { locating, failure, locate, clearFailure } = useLocate();
  const [dropping, setDropping] = useState(false);
  const [dropFailed, setDropFailed] = useState(false);
  const located = location.kind === "located";
  const offerLocate = canLocate && canGeolocate();
  const error =
    failure !== null
      ? t(failureText[failure])
      : dropFailed
        ? t("today.location.dropFailed")
        : "";

  async function takeCurrent() {
    setDropFailed(false);
    if ((await locate()) === null) return;
    setOpen(false);
    onChanged();
  }

  async function drop() {
    if (dropping) return;
    setDropping(true);
    setDropFailed(false);
    clearFailure();
    try {
      await dropLocated();
      setOpen(false);
      onChanged();
    } catch {
      setDropFailed(true);
    } finally {
      setDropping(false);
    }
  }

  return (
    <div ref={ref} className="relative">
      <button
        aria-controls={open ? dialogId : undefined}
        aria-expanded={open}
        aria-haspopup="dialog"
        className={`ui-control-text flex items-center gap-1.5 rounded-full text-[13px] transition-colors hover:bg-raised ${
          compact
            ? "h-8 px-2 text-muted"
            : "h-[30px] border border-border px-2.5 text-ink-warm"
        }`}
        onClick={() => setOpen((value) => !value)}
        type="button"
      >
        <PinIcon />
        {location.name}
        {/* The space is for the accessible name; flex drops it on screen. */}
        {!compact && " "}
        {!compact && (
          <span className="text-muted">
            ·{" "}
            {t(
              located
                ? "today.location.fromLocation"
                : "today.location.fromMemories",
            )}
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
          } w-[min(320px,calc(100vw-32px))] flex-col gap-2.5 rounded-2xl border border-[rgba(226,225,218,0.15)] bg-active p-4 text-sm text-ink-warm shadow-[0_12px_32px_rgba(0,0,0,0.4)]`}
          id={dialogId}
          role="dialog"
        >
          <div className="font-medium text-ink">
            {t("today.location.title")}
          </div>
          <p className="text-ink-dim">
            {t(
              located
                ? "today.location.located"
                : location.kind === "current"
                  ? "today.location.current"
                  : "today.location.home",
              { name: location.name },
            )}{" "}
            {t(
              located
                ? "today.location.locatedNote"
                : offerLocate
                  ? "today.location.changeLocate"
                  : "today.location.change",
            )}
          </p>
          {located && credit !== null && credit.text !== "" && (
            <div className="text-xs text-muted">
              {credit.url ? (
                <a
                  className="underline-offset-2 hover:underline"
                  href={credit.url}
                  rel="noreferrer"
                  target="_blank"
                >
                  {credit.text}
                </a>
              ) : (
                credit.text
              )}
            </div>
          )}
          {(offerLocate || located) && (
            <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
              {offerLocate && (
                <LocateButton
                  disabled={dropping}
                  locating={locating}
                  onClick={() => void takeCurrent()}
                />
              )}
              {located && (
                <button
                  className="ui-control-text h-8 rounded-lg bg-[rgba(255,255,255,0.1)] px-3 text-[13px] text-ink transition-colors hover:bg-[rgba(255,255,255,0.16)] disabled:opacity-50"
                  disabled={dropping || locating}
                  onClick={() => void drop()}
                  type="button"
                >
                  {t("today.location.backToMemories")}
                </button>
              )}
            </div>
          )}
          {error !== "" && (
            <div className="text-[13px] text-danger" role="alert">
              {error}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
