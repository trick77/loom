import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import type {
  TodayLocation,
  TodaySun,
  TodayWarning,
  TodayWeather,
} from "../api/today";
import { Icon } from "../chat/Icon";
import { degrees } from "./format";
import { LocationChip } from "./LocationChip";
import { conditionFor, conditionLabelKey, isNightNow } from "./weatherCodes";
import { SunTimeIcon, WarningIcon, WeatherIcon } from "./WeatherIcon";

function Divider() {
  return <span aria-hidden className="h-3.5 w-px bg-[#343432]" />;
}

// sunText is the next sun time worth knowing: the sunrise before it, the
// sunset after.
function sunText(sun: TodaySun, t: TFunction): string | null {
  if (sun.polar === "day") return t("today.sun.polarDay");
  if (sun.polar === "night") return t("today.sun.polarNight");
  if (sun.progress < 0 && sun.rise !== null)
    return t("today.sun.sunriseAt", { time: sun.rise });
  if (sun.set !== null) return t("today.sun.sunsetAt", { time: sun.set });
  return null;
}

function SunPart({ sun }: { sun: TodaySun }) {
  const { t } = useTranslation();
  const text = sunText(sun, t);
  if (text === null) return null;
  return (
    <span className="flex items-center gap-1.5">
      <SunTimeIcon rising={sun.polar === "" && sun.progress < 0} />
      <span className="text-[#e8e6dc]">{text}</span>
    </span>
  );
}

// TodayLine is the weather folded into one line: the sky now, a warning in
// the accent color, the next sun time and the place. It unfolds the tiles.
export function TodayLine({
  weather,
  sun,
  warning,
  location,
  expanded,
  onToggle,
  onOpenMemory,
}: {
  weather: TodayWeather;
  sun: TodaySun | null;
  warning: TodayWarning | null;
  location: TodayLocation | null;
  expanded: boolean;
  onToggle(): void;
  onOpenMemory(): void;
}) {
  const { t } = useTranslation();
  const condition = conditionFor(weather.now.code);
  const night = isNightNow(sun);
  return (
    <div className="flex max-w-full flex-wrap items-center justify-center gap-x-1 gap-y-1">
      <button
        aria-expanded={expanded}
        className="flex max-w-full flex-wrap items-center justify-center gap-x-[18px] gap-y-2 rounded-full px-3.5 py-2 text-[13px] text-[#97958c] transition-colors hover:bg-[#2a2a28]"
        onClick={onToggle}
        type="button"
      >
        <span className="flex items-center gap-1.5">
          <WeatherIcon condition={condition} night={night} size={18} />
          <span className="text-[#e8e6dc]">
            {t("today.temperature", {
              value: degrees(weather.now.temperature),
            })}{" "}
            {t(conditionLabelKey(condition, night))}
          </span>
        </span>
        {warning !== null && (
          <span
            className="flex items-center gap-1.5 font-medium text-accent-strong"
            data-testid="line-warning"
          >
            <WarningIcon kind={warning.kind} size={16} />
            {t(`today.warning.${warning.kind}`, { from: warning.from })}
          </span>
        )}
        {sun !== null && (
          <>
            <Divider />
            <SunPart sun={sun} />
          </>
        )}
        <span className="text-[#97958c]">
          <Icon
            className={`transition-transform ${expanded ? "rotate-180" : ""}`}
            name="chevronDown"
            size="14px"
          />
        </span>
      </button>
      {location !== null && (
        <LocationChip compact location={location} onOpenMemory={onOpenMemory} />
      )}
    </div>
  );
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
  return (
    <div
      aria-label={t("today.title")}
      className="flex flex-wrap items-center justify-center gap-x-[18px] gap-y-2 text-[13px] text-[#97958c]"
      role="group"
    >
      {both ? (
        <>
          <span className="flex items-center gap-1.5">
            <SunTimeIcon rising />
            <span className="text-[#e8e6dc]">
              {t("today.sun.sunriseAt", { time: sun.rise })}
            </span>
          </span>
          <Divider />
          <span className="flex items-center gap-1.5">
            <SunTimeIcon rising={false} />
            <span className="text-[#e8e6dc]">
              {t("today.sun.sunsetAt", { time: sun.set })}
            </span>
          </span>
        </>
      ) : (
        <SunPart sun={sun} />
      )}
      {location !== null && (
        <LocationChip compact location={location} onOpenMemory={onOpenMemory} />
      )}
    </div>
  );
}
