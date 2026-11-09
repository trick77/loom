import { useTranslation } from "react-i18next";

import type {
  TodayAir,
  TodayDetails,
  TodayOutlookDay,
  TodayPollen,
  TodayRain,
} from "../api/today";
import { degrees } from "./format";
import { conditionFor, conditionLabelKey } from "./weatherCodes";
import { WeatherIcon } from "./WeatherIcon";

const COMPASS = ["n", "ne", "e", "se", "s", "sw", "w", "nw"] as const;

// compassKey is the eight-point direction a wind blows from.
export function compassKey(degreesFrom: number): (typeof COMPASS)[number] {
  const i = Math.round((((degreesFrom % 360) + 360) % 360) / 45) % 8;
  return COMPASS[i];
}

// uvLevelKey is the WHO band of a UV index.
export function uvLevelKey(uv: number): string {
  const v = Math.round(uv);
  if (v <= 2) return "low";
  if (v <= 5) return "moderate";
  if (v <= 7) return "high";
  if (v <= 10) return "veryHigh";
  return "extreme";
}

// oneDecimal shows mm and hours to a tenth, a whole number without ".0".
function oneDecimal(value: number, language: string): string {
  return new Intl.NumberFormat(language, { maximumFractionDigits: 1 }).format(
    value,
  );
}

// DetailsCard is the shown day in numbers, each only when the forecast has it.
export function DetailsCard({
  details,
  tomorrow,
}: {
  details: TodayDetails;
  tomorrow: boolean;
}) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const items: { key: string; label: string; value: string }[] = [];
  if (details.feelsLike !== null)
    items.push({
      key: "feelsLike",
      label: t("today.details.feelsLike"),
      value: t("today.temperature", { value: degrees(details.feelsLike) }),
    });
  if (details.humidity !== null)
    items.push({
      key: "humidity",
      label: t("today.details.humidity"),
      value: t("today.percent", { percent: Math.round(details.humidity) }),
    });
  if (details.wind !== null) {
    const direction =
      details.windDirection === null
        ? ""
        : ` ${t(`today.compass.${compassKey(details.windDirection)}`)}`;
    items.push({
      key: "wind",
      label: t(tomorrow ? "today.details.windMax" : "today.details.wind"),
      value:
        t("today.details.speed", { speed: Math.round(details.wind) }) +
        direction,
    });
  }
  if (details.gusts !== null)
    items.push({
      key: "gusts",
      label: t("today.details.gusts"),
      value: t("today.details.speed", { speed: Math.round(details.gusts) }),
    });
  if (details.uvMax !== null)
    items.push({
      key: "uv",
      label: t("today.details.uv"),
      value: `${Math.round(details.uvMax)} · ${t(`today.uv.${uvLevelKey(details.uvMax)}`)}`,
    });
  if (details.precipitationSum !== null)
    items.push({
      key: "precipitation",
      label: t("today.details.precipitation"),
      value: t("today.details.mm", {
        mm: oneDecimal(details.precipitationSum, lang),
      }),
    });
  if (details.sunshineHours !== null)
    items.push({
      key: "sunshine",
      label: t("today.details.sunshine"),
      value: t("today.details.sunHours", {
        h: oneDecimal(details.sunshineHours, lang),
      }),
    });
  return (
    <dl className="grid grid-cols-3 gap-x-3 gap-y-2.5">
      {items.map((item) => (
        <div className="flex min-w-0 flex-col" key={item.key}>
          <dt className="text-[12px] text-muted">{item.label}</dt>
          <dd className="truncate text-[15px] text-ink">{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

// RAIN_SCALE_MM is the least an hour's bar is drawn against: drizzle stays a
// stub, not a full bar.
const RAIN_SCALE_MM = 2;

// RAIN_DRY_CHANCE is the chance below which hours with no amount are dry:
// the chance and the amount come from different models, so "no rain" needs
// both.
const RAIN_DRY_CHANCE = 20;

// RainCard is the rain hour by hour: bars by amount, faint where unlikely;
// by chance when the forecast has no amounts, or none above 0 mm.
export function RainCard({ rain }: { rain: TodayRain[] }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const amounts = rain.map((hour) => hour.precipitation ?? 0);
  const total = amounts.reduce((sum, mm) => sum + mm, 0);
  const byAmount = total > 0;
  const hasChance = rain.some((hour) => hour.probability !== null);
  const likeliest = Math.max(0, ...rain.map((hour) => hour.probability ?? 0));
  const scale = Math.max(RAIN_SCALE_MM, ...amounts);
  const summary = byAmount
    ? hasChance
      ? t("today.rain.total", {
          mm: oneDecimal(total, lang),
          percent: Math.round(likeliest),
        })
      : t("today.rain.amount", { mm: oneDecimal(total, lang) })
    : likeliest >= RAIN_DRY_CHANCE
      ? t("today.rain.chance", { percent: Math.round(likeliest) })
      : t("today.rain.dry");
  return (
    <div className="flex h-full flex-col gap-2">
      <p className="text-[13px] text-ink-soft" data-testid="rain-summary">
        {summary}
      </p>
      <ul
        aria-label={t("today.rain.hours")}
        className="flex min-h-16 flex-1 items-end gap-[3px]"
      >
        {rain.map((hour, i) => {
          const chance = hour.probability ?? (byAmount ? 100 : 0);
          const height = byAmount
            ? ((hour.precipitation ?? 0) / scale) * 100
            : chance;
          const label = [
            hour.time,
            hour.precipitation === null
              ? null
              : t("today.details.mm", {
                  mm: oneDecimal(hour.precipitation, lang),
                }),
            hour.probability === null
              ? null
              : t("today.percent", { percent: Math.round(hour.probability) }),
          ]
            .filter(Boolean)
            .join(" · ");
          return (
            <li
              aria-label={label}
              className="flex h-full flex-1 flex-col justify-end gap-1"
              key={hour.time}
              title={label}
            >
              <span className="relative flex h-full items-end overflow-hidden rounded-[3px] bg-track/40">
                <span
                  className="w-full rounded-[3px] bg-accent"
                  style={{
                    height: `${Math.max(height > 0 ? 6 : 0, Math.min(100, height))}%`,
                    opacity: 0.35 + 0.65 * Math.min(1, chance / 100),
                  }}
                />
              </span>
              <span className="h-3.5 text-center text-[10px] leading-3.5 text-muted">
                {i % 3 === 0 ? hour.time.slice(0, 2) : ""}
              </span>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// weekday is a "YYYY-MM-DD" as a short weekday, built as a local date.
function weekday(date: string, language: string): string {
  const [y, m, d] = date.split("-").map(Number);
  if (!y || !m || !d) return date;
  try {
    return new Intl.DateTimeFormat(language, { weekday: "short" }).format(
      new Date(y, m - 1, d),
    );
  } catch {
    return date;
  }
}

// OutlookCard is the days after the shown one: weekday, sky, chance of
// rain, and the range as a bar on a scale shared by all the days.
export function OutlookCard({ days }: { days: TodayOutlookDay[] }) {
  const { t, i18n } = useTranslation();
  const lowest = Math.min(...days.map((day) => day.low));
  const highest = Math.max(...days.map((day) => day.high));
  const span = Math.max(1, highest - lowest);
  return (
    <ul
      aria-label={t("today.cards.outlook")}
      className="flex flex-col gap-1.5 text-[13px]"
    >
      {days.map((day) => {
        const condition = conditionFor(day.code);
        const rain = day.precipitationProbability;
        return (
          <li className="flex items-center gap-2.5" key={day.date}>
            <span className="w-9 shrink-0 text-ink-soft">
              {weekday(day.date, i18n.language)}
            </span>
            <WeatherIcon
              condition={condition}
              label={t(conditionLabelKey(condition, false))}
              size={18}
            />
            <span className="w-9 shrink-0 text-right text-[12px] text-muted">
              {rain !== null && rain >= 10
                ? t("today.percent", { percent: Math.round(rain) })
                : ""}
            </span>
            <span className="w-7 shrink-0 text-right text-muted">
              {t("today.temperature", { value: degrees(day.low) })}
            </span>
            <span
              aria-hidden="true"
              className="relative h-1 min-w-8 flex-1 rounded-full bg-track"
            >
              <span
                className="absolute inset-y-0 rounded-full bg-ink-dim"
                style={{
                  left: `${((day.low - lowest) / span) * 100}%`,
                  right: `${((highest - day.high) / span) * 100}%`,
                }}
              />
            </span>
            <span className="w-7 shrink-0 text-ink">
              {t("today.temperature", { value: degrees(day.high) })}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

const POLLEN_STEPS: Record<TodayPollen["level"], number> = {
  low: 1,
  moderate: 2,
  high: 3,
  veryHigh: 4,
};

// AirCard is the air quality index with its band and the pollen worth a
// load class, each as four steps; tomorrow the index is the day's highest.
export function AirCard({ air }: { air: TodayAir }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-3">
      {air.aqi !== null && (
        <div className="flex flex-col">
          <span className="text-[12px] text-muted">
            {t(air.max ? "today.air.qualityMax" : "today.air.quality")}
          </span>
          <span className="text-[15px] text-ink" data-testid="air-quality">
            {air.level === ""
              ? Math.round(air.aqi)
              : `${Math.round(air.aqi)} · ${t(`today.air.levels.${air.level}`)}`}
          </span>
        </div>
      )}
      {air.pollen === null ? null : air.pollen.length === 0 ? (
        <p className="text-[13px] text-muted">{t("today.air.noPollen")}</p>
      ) : (
        <ul
          aria-label={t("today.air.pollen")}
          className="flex flex-col gap-1.5 text-[13px]"
        >
          {air.pollen.map((pollen) => {
            const steps = POLLEN_STEPS[pollen.level];
            return (
              <li className="flex items-center gap-3" key={pollen.kind}>
                <span className="w-20 shrink-0 text-ink-soft">
                  {t(`today.air.kinds.${pollen.kind}`)}
                </span>
                <span aria-hidden="true" className="flex gap-0.5">
                  {[1, 2, 3, 4].map((step) => (
                    <span
                      className={`h-1.5 w-4 rounded-full ${
                        step > steps
                          ? "bg-track"
                          : steps >= 3
                            ? "bg-accent"
                            : "bg-ink-dim"
                      }`}
                      key={step}
                    />
                  ))}
                </span>
                <span className="text-muted">
                  {t(`today.air.pollenLevels.${pollen.level}`)}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
