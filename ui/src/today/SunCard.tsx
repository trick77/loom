import type { TFunction } from "i18next";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import type { TodaySun } from "../api/today";
import { formatDuration } from "./format";
import { useFirstSight } from "./useFirstSight";

// The arc is the upper half of an ellipse from sunrise (left) to sunset
// (right) over the horizon line.
const CX = 110;
const CY = 74;
const RX = 90;
const RY = 62;
// Room under the horizon for the sun's glow (r 11) at rise and set.
const HEIGHT = CY + 11;

// sunPoint is where the sun stands on the arc at progress p (0 rise, 1 set).
export function sunPoint(p: number): { x: number; y: number } {
  const clamped = Math.min(1, Math.max(0, p));
  const angle = Math.PI * (1 - clamped);
  return {
    x: Math.round((CX + RX * Math.cos(angle)) * 10) / 10,
    y: Math.round((CY - RY * Math.sin(angle)) * 10) / 10,
  };
}

// SWEEP_MS is how long the sun takes from sunrise to where it stands.
export const SWEEP_MS = 1200;

// sweepAt is the sun's progress elapsed ms into its sweep to target: fast
// off the horizon, settling at its place (ease-out cubic).
export function sweepAt(target: number, elapsed: number): number {
  const share = Math.min(1, Math.max(0, elapsed / SWEEP_MS));
  return share === 1 ? target : target * (1 - (1 - share) ** 3);
}

// useSunSweep is the progress to draw: the sun runs from sunrise to target
// the first time the card is seen (useFirstSight); until then it waits at
// sunrise, where the sweep starts. Reduced motion sweeps all the same: the
// sweep plays once and tells where the sun stands. No observer: target at
// once. Not enabled (no sun drawn): no sweep, and a sun that rises later
// still gets its own.
function useSunSweep(target: number, enabled: boolean) {
  const { ref, seen } = useFirstSight<SVGSVGElement>(enabled);
  // ms into the sweep, from the moment the card is seen.
  const [elapsed, setElapsed] = useState(() =>
    typeof IntersectionObserver === "undefined" ? SWEEP_MS : 0,
  );
  const running = enabled && seen && elapsed < SWEEP_MS;
  useEffect(() => {
    if (!running) return;
    let start: number | null = null;
    let frame = requestAnimationFrame(function step(now) {
      start ??= now;
      const ms = now - start;
      setElapsed(ms);
      if (ms < SWEEP_MS) frame = requestAnimationFrame(step);
    });
    return () => cancelAnimationFrame(frame);
  }, [running]);
  const shown = !enabled ? target : !seen ? 0 : sweepAt(target, elapsed);
  return { ref, shown };
}

function positionLabel(sun: TodaySun, t: TFunction): string {
  if (sun.polar === "day") return t("today.sun.polarDay");
  if (sun.polar === "night") return t("today.sun.polarNight");
  if (sun.progress === null)
    return t("today.sun.times", {
      rise: sun.rise ?? "–",
      set: sun.set ?? "–",
    });
  if (sun.progress < 0) return t("today.sun.beforeRise");
  if (sun.progress > 1) return t("today.sun.afterSet");
  return t("today.sun.up", {
    duration: formatDuration((1 - sun.progress) * sun.dayMinutes, t),
  });
}

// SunCard draws the sun's arc with its position now (not for tomorrow), and
// under it the sun times with the day's length between them: on a line of
// its own the length took the card a third taller than every other card.
export function SunCard({
  sun,
  day,
}: {
  sun: TodaySun;
  day: "today" | "tomorrow";
}) {
  const { t } = useTranslation();
  const progress = sun.progress;
  const duration = formatDuration(sun.dayMinutes, t);
  const dayLength = t("today.sun.dayLength", { duration });
  const up =
    sun.polar === "day" ||
    (sun.polar === "" && progress !== null && progress >= 0 && progress <= 1);
  // Tomorrow has no "now": no dot on the arc.
  const dot = up && day === "today";
  const { ref, shown } = useSunSweep(
    sun.polar === "day" ? 0.5 : (progress ?? 0),
    dot,
  );
  const point = sunPoint(shown);
  // A polar day lights the whole arc; otherwise it fills up to the sun.
  const lit =
    sun.polar === "day"
      ? `M${CX - RX} ${CY} A${RX} ${RY} 0 0 1 ${CX + RX} ${CY}`
      : `M${CX - RX} ${CY} A${RX} ${RY} 0 0 1 ${point.x} ${point.y}`;
  return (
    <div className="flex flex-col gap-2.5">
      <svg
        ref={ref}
        viewBox={`0 0 220 ${HEIGHT}`}
        width="100%"
        height={HEIGHT}
        role="img"
        aria-label={positionLabel(sun, t)}
      >
        <line
          x1="6"
          y1={CY}
          x2="214"
          y2={CY}
          className="stroke-track"
          strokeWidth="1"
        />
        <path
          d={`M${CX - RX} ${CY} A${RX} ${RY} 0 0 1 ${CX + RX} ${CY}`}
          fill="none"
          className="stroke-track"
          strokeWidth="2"
          strokeDasharray="3 5"
        />
        {up && (
          <>
            <path
              d={lit}
              fill="none"
              className="stroke-sun"
              strokeWidth="2.5"
              strokeLinecap="round"
            />
          </>
        )}
        {dot && (
          <>
            <circle
              cx={point.x}
              cy={point.y}
              r="6"
              className="fill-sun"
              data-testid="sun-dot"
            />
            <circle
              cx={point.x}
              cy={point.y}
              r="11"
              className="fill-sun"
              opacity="0.18"
            />
          </>
        )}
      </svg>
      {sun.polar === "" ? (
        // Two rows, not three columns: the labels ("Sonnenuntergang") need
        // the full width, the length fits between the times.
        <div className="flex flex-col text-[13px]">
          <div className="flex justify-between gap-3 text-muted">
            <span>{t("today.sun.sunrise")}</span>
            <span className="text-right">{t("today.sun.sunset")}</span>
          </div>
          <div className="flex items-baseline justify-between gap-3">
            <span className="text-lg text-ink">{sun.rise ?? "–"}</span>
            <span className="text-muted">
              <span className="sr-only">{dayLength}</span>
              <span aria-hidden="true" title={dayLength}>
                {duration}
              </span>
            </span>
            <span className="text-lg text-ink">{sun.set ?? "–"}</span>
          </div>
        </div>
      ) : (
        // A polar day or night says its length by its name.
        <div className="text-lg text-ink">
          {t(
            sun.polar === "day" ? "today.sun.polarDay" : "today.sun.polarNight",
          )}
        </div>
      )}
    </div>
  );
}
