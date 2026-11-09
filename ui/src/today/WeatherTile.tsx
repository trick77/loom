import { useTranslation } from "react-i18next";

import type { TodaySun, TodayWarning, TodayWeather } from "../api/today";
import { degrees } from "./format";
import {
  conditionFor,
  conditionLabelKey,
  isNightAt,
  isNightNow,
  warned,
} from "./weatherCodes";
import { WeatherIcon } from "./WeatherIcon";

export const TILE =
  "rounded-2xl border border-[#323230] bg-[#242422] p-[18px] text-[#faf9f5]";

// WeatherTile shows the sky now, today's range and the next hours. A warning
// takes the condition's place, in the accent color.
export function WeatherTile({
  weather,
  sun,
  warning,
}: {
  weather: TodayWeather;
  sun: TodaySun | null;
  warning: TodayWarning | null;
}) {
  const { t } = useTranslation();
  const condition = conditionFor(weather.now.code);
  const night = isNightNow(sun);
  const label = t(conditionLabelKey(condition, night));
  const rain = weather.today.precipitationProbability;
  const range =
    t("today.highLow", {
      high: degrees(weather.today.high),
      low: degrees(weather.today.low),
    }) +
    (rain === null
      ? ""
      : ` · ${t("today.rainChance", { percent: Math.round(rain) })}`);
  return (
    <div className={`${TILE} flex flex-col gap-4`}>
      <div className="flex items-center gap-3.5">
        <WeatherIcon
          condition={condition}
          night={night}
          size={40}
          label={label}
        />
        <div className="flex min-w-0 items-baseline gap-2.5">
          <span className="text-4xl leading-none font-light">
            {t("today.temperature", {
              value: degrees(weather.now.temperature),
            })}
          </span>
          <span className="flex min-w-0 flex-col text-[13px] leading-[1.35]">
            {warning === null ? (
              <span className="text-[#e8e6dc]">{label}</span>
            ) : (
              <span
                className="font-medium text-[#d97757]"
                data-testid="weather-warning"
              >
                {t(`today.warning.${warning.kind}`, { from: warning.from })}
              </span>
            )}
            <span className="text-[#97958c]">{range}</span>
          </span>
        </div>
      </div>
      {weather.hours.length > 0 && (
        <ul
          aria-label={t("today.hours")}
          className="grid grid-cols-5 gap-1 text-center text-[13px]"
        >
          {weather.hours.slice(0, 5).map((hour) => {
            const hourCondition = conditionFor(hour.code);
            const hourNight = isNightAt(hour.time, sun);
            return (
              <li
                key={hour.time}
                className="flex flex-col items-center gap-1"
                title={t(conditionLabelKey(hourCondition, hourNight))}
              >
                <span className="text-[#97958c]">{hour.time}</span>
                <WeatherIcon
                  condition={hourCondition}
                  night={hourNight}
                  size={20}
                  warn={warned(hourCondition, warning)}
                />
                <span className="text-[#e8e6dc]">
                  {t("today.temperature", { value: degrees(hour.temperature) })}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
