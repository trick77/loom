import { useTranslation } from "react-i18next";

import type { Today, TodayWeather } from "../api/today";
import { Carousel, type Slide } from "./Carousel";
import { degrees } from "./format";
import { DetailsCard, OutlookCard, RainCard } from "./WeatherCards";
import {
  conditionFor,
  conditionLabelKey,
  isNightAt,
  isNightNow,
  warned,
} from "./weatherCodes";
import { WarningIcon, WeatherIcon } from "./WeatherIcon";

export const TILE =
  "rounded-2xl border border-border bg-card p-[18px] text-ink";

type WeatherTileProps = {
  weather: TodayWeather;
  today: Pick<Today, "day" | "sun" | "warning">;
};

// WeatherTile is the weather's cards to turn through: the sky now (or
// tomorrow's) first, then whatever further cards the forecast has values
// for.
export function WeatherTile({ weather, today }: WeatherTileProps) {
  const { t } = useTranslation();
  const tomorrow = today.day === "tomorrow";
  const slides: Slide[] = [
    {
      key: "now",
      name: t(tomorrow ? "today.cards.overview" : "today.cards.now"),
      content: <NowCard today={today} weather={weather} />,
    },
  ];
  if (weather.details !== null)
    slides.push({
      key: "details",
      name: t("today.cards.details"),
      content: <DetailsCard details={weather.details} tomorrow={tomorrow} />,
    });
  if (weather.rain !== null)
    slides.push({
      key: "rain",
      name: t("today.cards.rain"),
      content: <RainCard rain={weather.rain} />,
    });
  if (weather.outlook !== null)
    slides.push({
      key: "outlook",
      name: t("today.cards.outlook"),
      content: <OutlookCard days={weather.outlook} />,
    });
  return (
    <div className={TILE}>
      <Carousel label={t("today.cards.weather")} slides={slides} />
    </div>
  );
}

// NowCard shows the sky now with the day's range, or for tomorrow the day's
// high and low, then the hours. A warning takes the condition's place, in the
// accent color.
function NowCard({ weather, today }: WeatherTileProps) {
  const { t } = useTranslation();
  const { sun, warning } = today;
  const now = today.day === "today" ? weather.now : null;
  const condition = conditionFor(now?.code ?? weather.day.code);
  const night = now !== null && isNightNow(sun);
  const label = t(conditionLabelKey(condition, night));
  const rain = weather.day.precipitationProbability;
  const rainText =
    rain === null ? null : t("today.rainChance", { percent: Math.round(rain) });
  // Now: the range goes under the label. Tomorrow: the range is the big
  // value, the rain chance stays.
  const detail =
    now === null
      ? rainText
      : t("today.highLow", {
          high: degrees(weather.day.high),
          low: degrees(weather.day.low),
        }) + (rainText === null ? "" : ` · ${rainText}`);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3.5">
        <WeatherIcon
          condition={condition}
          night={night}
          size={40}
          label={label}
        />
        <div className="flex min-w-0 items-baseline gap-2.5">
          {now !== null ? (
            <span className="text-4xl leading-none font-light">
              {t("today.temperature", { value: degrees(now.temperature) })}
            </span>
          ) : (
            <span
              className="shrink-0 leading-none font-light"
              data-testid="day-range"
            >
              <span className="text-4xl">
                {t("today.temperature", {
                  value: degrees(weather.day.high),
                })}
              </span>
              <span className="text-xl text-muted">
                {" / "}
                {t("today.temperature", { value: degrees(weather.day.low) })}
              </span>
            </span>
          )}
          <span className="flex min-w-0 flex-col text-[13px] leading-[1.35]">
            {warning === null ? (
              <span className="text-ink-soft">{label}</span>
            ) : (
              <span
                className="flex items-center gap-1 font-medium text-accent-strong"
                data-testid="weather-warning"
              >
                <WarningIcon kind={warning.kind} size={14} />
                {t(
                  // Its own day: in the evening a warning can be for tonight.
                  `today.${warning.day === "tomorrow" ? "warningTomorrow" : "warning"}.${warning.kind}`,
                  { from: warning.from },
                )}
              </span>
            )}
            {detail !== null && <span className="text-muted">{detail}</span>}
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
                <span className="text-muted">{hour.time}</span>
                <WeatherIcon
                  condition={hourCondition}
                  night={hourNight}
                  size={20}
                  warn={warned(hourCondition, warning)}
                />
                <span className="text-ink-soft">
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
