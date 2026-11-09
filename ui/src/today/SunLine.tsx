import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import type { TodayLocation, TodaySun } from "../api/today";
import { LocationChip } from "./LocationChip";
import { SunTimeIcon } from "./WeatherIcon";

function Divider() {
  return <span aria-hidden className="h-3.5 w-px bg-border" />;
}

// nextIsSunrise: before sunrise and after sunset the next sun time is a
// sunrise. After sunset today's stands in for tomorrow's: they differ by
// minutes.
function nextIsSunrise(sun: TodaySun): boolean {
  return (
    sun.polar === "" &&
    sun.progress !== null &&
    (sun.progress < 0 || sun.progress > 1)
  );
}

// sunText is the one sun time worth knowing when not both are known.
function sunText(sun: TodaySun, t: TFunction): string | null {
  if (sun.polar === "day") return t("today.sun.polarDay");
  if (sun.polar === "night") return t("today.sun.polarNight");
  if (nextIsSunrise(sun) && sun.rise !== null)
    return t("today.sun.sunriseAt", { time: sun.rise });
  if (sun.set !== null) return t("today.sun.sunsetAt", { time: sun.set });
  if (sun.rise !== null) return t("today.sun.sunriseAt", { time: sun.rise });
  return null;
}

// SunLine is the sun times alone, for a place without weather: under the
// prompt starters, or above the schedule news.
export function SunLine({
  sun,
  location,
  onOpenMemory,
}: {
  sun: TodaySun;
  location: TodayLocation | null;
  onOpenMemory(): void;
}) {
  const { t } = useTranslation();
  const both = sun.polar === "" && sun.rise !== null && sun.set !== null;
  const single = both ? null : sunText(sun, t);
  return (
    <div
      aria-label={t("today.title")}
      className="flex flex-wrap items-center justify-center gap-x-[18px] gap-y-2 text-[13px] text-muted"
      role="group"
    >
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
            <SunTimeIcon rising={nextIsSunrise(sun)} />
            <span className="text-ink-soft">{single}</span>
          </span>
        )
      )}
      {location !== null && (
        <LocationChip compact location={location} onOpenMemory={onOpenMemory} />
      )}
    </div>
  );
}
