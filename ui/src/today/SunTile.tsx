import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import type { TodaySun } from "../api/today";
import { formatDuration } from "./format";
import { TILE } from "./WeatherTile";

// The arc is the upper half of an ellipse from sunrise (left) to sunset
// (right) over the horizon line.
const CX = 110;
const CY = 86;
const RX = 90;
const RY = 74;

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

// dayLengthLine is "Day length 9 h 33 min · 2 min shorter than yesterday";
// for tomorrow the day before is today.
export function dayLengthLine(
  sun: TodaySun,
  day: "today" | "tomorrow",
  t: TFunction,
): string {
  const delta = Math.round(sun.deltaMinutes);
  const tomorrow = day === "tomorrow";
  const change =
    delta > 0
      ? t(tomorrow ? "today.sun.longerThanToday" : "today.sun.longer", {
          count: delta,
        })
      : delta < 0
        ? t(tomorrow ? "today.sun.shorterThanToday" : "today.sun.shorter", {
            count: -delta,
          })
        : t(tomorrow ? "today.sun.sameAsToday" : "today.sun.same");
  return `${t("today.sun.dayLength", { duration: formatDuration(sun.dayMinutes, t) })} · ${change}`;
}

// SunTile draws the sun's arc with its position now (not for tomorrow), the
// sun times and the day's length against the day before.
export function SunTile({
  sun,
  day,
}: {
  sun: TodaySun;
  day: "today" | "tomorrow";
}) {
  const { t } = useTranslation();
  const progress = sun.progress;
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
    <div className={`${TILE} flex flex-col gap-2.5`}>
      <svg
        viewBox="0 0 220 96"
        width="100%"
        height="96"
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
        <div className="flex justify-between text-[13px]">
          <span className="flex flex-col">
            <span className="text-muted">{t("today.sun.sunrise")}</span>
            <span className="text-lg text-ink">{sun.rise ?? "–"}</span>
          </span>
          <span className="flex flex-col text-right">
            <span className="text-muted">{t("today.sun.sunset")}</span>
            <span className="text-lg text-ink">{sun.set ?? "–"}</span>
          </span>
        </div>
      ) : (
        <div className="text-lg text-ink">
          {t(
            sun.polar === "day" ? "today.sun.polarDay" : "today.sun.polarNight",
          )}
        </div>
      )}
      <div className="text-[13px] text-muted">{dayLengthLine(sun, day, t)}</div>
    </div>
  );
}
