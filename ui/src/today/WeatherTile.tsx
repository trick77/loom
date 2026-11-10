import { useTranslation } from "react-i18next";

import type { Today, TodayWeather } from "../api/today";
import { Carousel, type Slide } from "./Carousel";
import { degrees } from "./format";
import { SunCard } from "./SunCard";
import { type Tile, useTileClock } from "./useTileClock";
import { AirCard, DetailsCard, OutlookCard, RainCard } from "./WeatherCards";
import {
  conditionFor,
  conditionLabelKey,
  isNightAt,
  isNightNow,
  warningKey,
} from "./weatherCodes";
import { WarningIcon, WeatherIcon } from "./WeatherIcon";

const TILE = "rounded-2xl border border-border bg-card p-[18px] text-ink";

type WeatherTileProps = {
  weather: TodayWeather;
  today: Pick<Today, "date" | "day" | "sun" | "warning" | "air">;
};

// WeatherTiles is the weather's cards over two tiles: the left the sky's
// course (now or tomorrow's, the rain, the days ahead), the right the
// conditions (the sun, the day in numbers, the air). Each card only when the
// forecast has values for it; a side without any leaves its tile out.
export function WeatherTiles({ weather, today }: WeatherTileProps) {
  // A new shown day starts over on the first cards, where its warning is:
  // the 20:00 turn to tomorrow keeps the page open.
  return (
    <TilePair
      key={`${today.date}/${today.day}`}
      today={today}
      weather={weather}
    />
  );
}

function TilePair({ weather, today }: WeatherTileProps) {
  const { t } = useTranslation();
  const tomorrow = today.day === "tomorrow";
  const course: Slide[] = [
    {
      key: "now",
      name: t(tomorrow ? "today.cards.overview" : "today.cards.now"),
      content: <NowCard today={today} weather={weather} />,
    },
  ];
  if (weather.rain !== null)
    course.push({
      key: "rain",
      name: t("today.cards.rain"),
      content: <RainCard outlook={weather.outlook} rain={weather.rain} />,
    });
  if (weather.outlook !== null)
    course.push({
      key: "outlook",
      name: t("today.cards.outlook"),
      content: <OutlookCard days={weather.outlook} />,
    });
  const conditions: Slide[] = [];
  if (today.sun !== null)
    conditions.push({
      key: "sun",
      name: t("today.cards.sun"),
      content: <SunCard day={today.day} sun={today.sun} />,
    });
  if (weather.details !== null)
    conditions.push({
      key: "details",
      name: t("today.cards.details"),
      content: <DetailsCard details={weather.details} tomorrow={tomorrow} />,
    });
  if (today.air)
    conditions.push({
      key: "air",
      name: t("today.cards.air"),
      content: <AirCard air={today.air} />,
    });

  const clock = useTileClock([course.length, conditions.length]);
  const tile = (n: Tile, label: string, slides: Slide[]) => (
    <div className={TILE}>
      <Carousel
        clock={{
          auto: clock.auto,
          running: clock.running,
          due: clock.due === n,
          epoch: clock.epoch,
        }}
        index={clock.index[n]}
        label={label}
        onHold={n === 0 ? clock.holdLeft : clock.holdRight}
        onTurn={(to) => clock.turn(n, to)}
        slides={slides}
      />
    </div>
  );
  return (
    <>
      {tile(0, t("today.cards.weather"), course)}
      {conditions.length > 0 &&
        tile(1, t("today.cards.conditions"), conditions)}
    </>
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
                {t(warningKey(today.day, warning), { from: warning.from })}
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
