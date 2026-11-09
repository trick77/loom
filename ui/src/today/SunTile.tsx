import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import type { TodaySun } from "../api/today";
import { formatDuration } from "./format";
import { SUN_COLOR } from "./WeatherIcon";
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
  if (sun.progress < 0) return t("today.sun.beforeRise");
  if (sun.progress > 1) return t("today.sun.afterSet");
  return t("today.sun.up", {
    duration: formatDuration((1 - sun.progress) * sun.dayMinutes, t),
  });
}

// dayLengthLine is "Day length 9 h 33 min · 2 min shorter than yesterday".
export function dayLengthLine(sun: TodaySun, t: TFunction): string {
  const delta = Math.round(sun.deltaMinutes);
  const change =
    delta > 0
      ? t("today.sun.longer", { count: delta })
      : delta < 0
        ? t("today.sun.shorter", { count: -delta })
        : t("today.sun.same");
  return `${t("today.sun.dayLength", { duration: formatDuration(sun.dayMinutes, t) })} · ${change}`;
}

// SunTile draws the sun's position on its arc, the sun times and the day's
// length against yesterday.
export function SunTile({ sun }: { sun: TodaySun }) {
  const { t } = useTranslation();
  const up =
    sun.polar === "day" ||
    (sun.polar === "" && sun.progress >= 0 && sun.progress <= 1);
  const point = sunPoint(sun.polar === "day" ? 0.5 : sun.progress);
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
          stroke="#3a3a37"
          strokeWidth="1"
        />
        <path
          d={`M${CX - RX} ${CY} A${RX} ${RY} 0 0 1 ${CX + RX} ${CY}`}
          fill="none"
          stroke="#3a3a37"
          strokeWidth="2"
          strokeDasharray="3 5"
        />
        {up && (
          <>
            <path
              d={lit}
              fill="none"
              stroke={SUN_COLOR}
              strokeWidth="2.5"
              strokeLinecap="round"
            />
            <circle
              cx={point.x}
              cy={point.y}
              r="6"
              fill={SUN_COLOR}
              data-testid="sun-dot"
            />
            <circle
              cx={point.x}
              cy={point.y}
              r="11"
              fill={SUN_COLOR}
              opacity="0.18"
            />
          </>
        )}
      </svg>
      {sun.polar === "" ? (
        <div className="flex justify-between text-[13px]">
          <span className="flex flex-col">
            <span className="text-[#97958c]">{t("today.sun.sunrise")}</span>
            <span className="text-lg text-[#faf9f5]">{sun.rise ?? "–"}</span>
          </span>
          <span className="flex flex-col text-right">
            <span className="text-[#97958c]">{t("today.sun.sunset")}</span>
            <span className="text-lg text-[#faf9f5]">{sun.set ?? "–"}</span>
          </span>
        </div>
      ) : (
        <div className="text-lg text-[#faf9f5]">
          {t(
            sun.polar === "day" ? "today.sun.polarDay" : "today.sun.polarNight",
          )}
        </div>
      )}
      <div className="text-[13px] text-[#97958c]">{dayLengthLine(sun, t)}</div>
    </div>
  );
}
