import type { TFunction } from "i18next";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";

import type { TodaySun } from "../api/today";
import { SunTimeIcon } from "./WeatherIcon";

function Divider() {
  return <span aria-hidden className="h-3.5 w-px bg-border" />;
}

type Day = "today" | "tomorrow";

// nextIsSunrise: today, before sunrise and after sunset the next sun time is
// a sunrise (after sunset today's stands in for tomorrow's: they differ by
// minutes). Tomorrow has no "now" to place.
function nextIsSunrise(sun: TodaySun, day: Day): boolean {
  return (
    day === "today" &&
    sun.polar === "" &&
    sun.progress !== null &&
    (sun.progress < 0 || sun.progress > 1)
  );
}

// sunText is the one sun time worth knowing when not both are known.
function sunText(sun: TodaySun, day: Day, t: TFunction): string | null {
  if (sun.polar === "day") return t("today.sun.polarDay");
  if (sun.polar === "night") return t("today.sun.polarNight");
  if (nextIsSunrise(sun, day) && sun.rise !== null)
    return t("today.sun.sunriseAt", { time: sun.rise });
  if (sun.set !== null) return t("today.sun.sunsetAt", { time: sun.set });
  if (sun.rise !== null) return t("today.sun.sunriseAt", { time: sun.rise });
  return null;
}

// SunLine is the sun times alone, for a place without weather: under the
// prompt starters, or above the schedule news. From the evening the times
// are tomorrow's, and the line says so.
export function SunLine({
  sun,
  day,
  chip,
}: {
  sun: TodaySun;
  day: Day;
  /** The place chip, compact; none without a place. */
  chip?: ReactNode;
}) {
  const { t } = useTranslation();
  const both = sun.polar === "" && sun.rise !== null && sun.set !== null;
  const single = both ? null : sunText(sun, day, t);
  const tomorrow = day === "tomorrow";
  return (
    <div
      aria-label={t(tomorrow ? "today.titleTomorrow" : "today.title")}
      className="flex flex-wrap items-center justify-center gap-x-[18px] gap-y-2 text-[13px] text-muted"
      role="group"
    >
      {tomorrow && (
        <span className="font-medium" data-testid="sun-line-day">
          {t("today.titleTomorrow")}
        </span>
      )}
      {both ? (
        <>
          <span className="flex items-center gap-1.5">
            <SunTimeIcon rising />
            <span className="text-ink-soft">
              {t("today.sun.sunriseAt", { time: sun.rise })}
            </span>
          </span>
          <Divider />
          <span className="flex items-center gap-1.5">
            <SunTimeIcon rising={false} />
            <span className="text-ink-soft">
              {t("today.sun.sunsetAt", { time: sun.set })}
            </span>
          </span>
        </>
      ) : (
        single !== null && (
          <span className="flex items-center gap-1.5">
            <SunTimeIcon rising={nextIsSunrise(sun, day)} />
            <span className="text-ink-soft">{single}</span>
          </span>
        )
      )}
      {chip}
    </div>
  );
}
