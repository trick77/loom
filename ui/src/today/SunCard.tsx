import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import type { TodaySun } from "../api/today";
import { formatDuration } from "./format";

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
  const point = sunPoint(sun.polar === "day" ? 0.5 : (progress ?? 0));
  // Tomorrow has no "now": no dot on the arc.
  const dot = up && day === "today";
  // A polar day lights the whole arc; otherwise it fills up to the sun.
  const lit =
    sun.polar === "day"
      ? `M${CX - RX} ${CY} A${RX} ${RY} 0 0 1 ${CX + RX} ${CY}`
      : `M${CX - RX} ${CY} A${RX} ${RY} 0 0 1 ${point.x} ${point.y}`;
  return (
    <div className="flex flex-col gap-2.5">
      <svg
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
