import { type ReactNode, useId } from "react";
import { useTranslation } from "react-i18next";

import type {
  TodayAir,
  TodayAirHour,
  TodayDetails,
  TodayOutlookDay,
  TodayPollen,
  TodayRain,
} from "../api/today";
import { degrees } from "./format";
import { conditionFor, conditionLabelKey } from "./weatherCodes";
import { MetricIcon, WeatherIcon } from "./WeatherIcon";

// Every card past the first is built like it: a lead (a 40 px picture, a
// big value, two lines) over a strip.

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

// UV bands take the air's colors: the same scale from fine to harmful.
const UV_TEXT: Record<string, string> = {
  low: "text-aq-good",
  moderate: "text-aq-moderate",
  high: "text-aq-poor",
  veryHigh: "text-aq-very-poor",
  extreme: "text-aq-extreme",
};

// oneDecimal shows mm and hours to a tenth, a whole number without ".0".
function oneDecimal(value: number, language: string): string {
  return new Intl.NumberFormat(language, { maximumFractionDigits: 1 }).format(
    value,
  );
}

// Lead is a card's first row, as on the first card: the picture, the value
// with its unit, a line and an optional second one.
function Lead({
  picture,
  value,
  unit,
  line,
  lineClass = "text-ink-soft",
  detail,
  small = false,
  testId,
}: {
  picture: ReactNode;
  value: string;
  unit?: string;
  line: string;
  lineClass?: string;
  detail?: string | null;
  small?: boolean;
  testId: string;
}) {
  return (
    <div className="flex items-center gap-3.5" data-testid={testId}>
      {picture}
      <div className="flex min-w-0 items-baseline gap-2.5">
        <span
          className={`shrink-0 leading-none font-light ${small ? "text-3xl" : "text-4xl"}`}
        >
          {value}
          {unit !== undefined && (
            <span className="ml-0.5 text-[15px] text-muted">{unit}</span>
          )}
        </span>
        <span className="flex min-w-0 flex-col text-[13px] leading-[1.35]">
          <span className={lineClass}>{line}</span>
          {detail && <span className="text-muted">{detail}</span>}
        </span>
      </div>
    </div>
  );
}

// WindDial is a compass ring with north marked and an arrow to where the
// wind blows; without a direction only the ring.
function WindDial({ from }: { from: number | null }) {
  // The line beside it says the direction: decoration.
  return (
    <svg
      aria-hidden="true"
      className="shrink-0"
      fill="none"
      height="40"
      viewBox="0 0 40 40"
      width="40"
    >
      <circle
        className="stroke-track"
        cx="20"
        cy="20"
        r="17"
        strokeWidth="1.5"
      />
      <path
        className="stroke-muted"
        d="M20 2.5v4"
        strokeLinecap="round"
        strokeWidth="1.5"
      />
      {from === null ? (
        <circle className="fill-ink-soft" cx="20" cy="20" r="2.5" />
      ) : (
        <g
          data-testid="wind-arrow"
          transform={`rotate(${Math.round((((from + 180) % 360) + 360) % 360)} 20 20)`}
        >
          <path
            className="stroke-ink-soft"
            d="M20 30V10M14.5 15.5 20 10l5.5 5.5"
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth="2"
          />
        </g>
      )}
    </svg>
  );
}

type Metric = {
  key: string;
  label: string;
  short: string;
  value: string;
  icon: ReactNode;
  valueClass?: string;
};

// DetailsCard is the shown day in numbers, each only when the forecast has
// it: the wind leads (else how it feels), the rest in a strip.
export function DetailsCard({
  details,
  tomorrow,
}: {
  details: TodayDetails;
  tomorrow: boolean;
}) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const icon = (
    kind: Parameters<typeof MetricIcon>[0]["kind"],
    cls = "text-ink-dim",
  ) => <MetricIcon className={cls} kind={kind} size={18} />;
  let lead: ReactNode = null;
  if (details.wind !== null) {
    const direction =
      details.windDirection === null
        ? null
        : t(`today.compass.${compassKey(details.windDirection)}`);
    const line =
      direction === null
        ? t(tomorrow ? "today.details.windMax" : "today.details.wind")
        : t(tomorrow ? "today.details.windMaxFrom" : "today.details.windFrom", {
            direction,
          });
    lead = (
      <Lead
        detail={
          details.gusts === null
            ? null
            : t("today.details.gustsUpTo", {
                speed: Math.round(details.gusts),
              })
        }
        line={line}
        picture={<WindDial from={details.windDirection} />}
        testId="details-hero"
        unit={t("today.details.kmh")}
        value={String(Math.round(details.wind))}
      />
    );
  } else if (details.feelsLike !== null) {
    lead = (
      <Lead
        line={t("today.details.feelsLike")}
        picture={
          <MetricIcon className="text-ink-dim" kind="thermometer" size={40} />
        }
        testId="details-hero"
        value={t("today.temperature", { value: degrees(details.feelsLike) })}
      />
    );
  }
  const items: Metric[] = [];
  if (details.feelsLike !== null && details.wind !== null)
    items.push({
      key: "feelsLike",
      label: t("today.details.feelsLike"),
      short: t("today.details.short.feelsLike"),
      value: t("today.temperature", { value: degrees(details.feelsLike) }),
      icon: icon("thermometer"),
    });
  if (details.humidity !== null)
    items.push({
      key: "humidity",
      label: t("today.details.humidity"),
      short: t("today.details.short.humidity"),
      value: t("today.percent", { percent: Math.round(details.humidity) }),
      icon: icon("droplet"),
    });
  if (details.gusts !== null && details.wind === null)
    items.push({
      key: "gusts",
      label: t("today.details.gusts"),
      short: t("today.details.short.gusts"),
      value: t("today.details.speed", { speed: Math.round(details.gusts) }),
      icon: icon("wind"),
    });
  if (details.uvMax !== null) {
    const level = uvLevelKey(details.uvMax);
    items.push({
      key: "uv",
      label: t("today.details.uv"),
      short: t("today.details.short.uv", { level: t(`today.uv.${level}`) }),
      value: String(Math.round(details.uvMax)),
      icon: icon("sun", UV_TEXT[level]),
      valueClass: UV_TEXT[level],
    });
  }
  if (details.precipitationSum !== null)
    items.push({
      key: "precipitation",
      label: t("today.details.precipitation"),
      short: t("today.details.short.precipitation"),
      value: t("today.details.mm", {
        mm: oneDecimal(details.precipitationSum, lang),
      }),
      icon: icon("rain", "text-rain"),
    });
  if (details.sunshineHours !== null)
    items.push({
      key: "sunshine",
      label: t("today.details.sunshine"),
      short: t("today.details.short.sunshine"),
      value: t("today.details.sunHours", {
        h: oneDecimal(details.sunshineHours, lang),
      }),
      icon: icon("sun", "text-sun"),
    });
  return (
    <div className="flex h-full flex-col justify-between gap-4">
      {lead}
      {items.length > 0 && (
        <dl
          className="grid gap-1 text-center"
          style={{
            gridTemplateColumns: `repeat(${items.length}, minmax(0, 1fr))`,
          }}
        >
          {items.map((item) => (
            <div
              className="flex min-w-0 flex-col items-center gap-0.5"
              key={item.key}
              title={item.label}
            >
              {item.icon}
              <dt className="order-last max-w-full truncate text-[11px] text-muted">
                {item.short}
              </dt>
              <dd
                className={`max-w-full truncate text-[14px] ${item.valueClass ?? "text-ink"}`}
              >
                {item.value}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}

// RAIN_SCALE_MM is the least an hour's bar is drawn against: drizzle stays a
// stub, not a full bar.
const RAIN_SCALE_MM = 2;

// RAIN_DRY_CHANCE is the chance below which hours with no amount are dry:
// the chance and the amount come from different models, so "no rain" needs
// both.
const RAIN_DRY_CHANCE = 20;

// NEXT_RAIN_CHANCE is the chance from which a day of the outlook is the next
// rain a dry card names (the warnings' threshold).
const NEXT_RAIN_CHANCE = 50;

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

// RainCard is the rain hour by hour: the amount leads, bars by amount, faint
// where unlikely; by chance when the forecast has no amounts, or none above
// 0 mm. A dry card names the next rainy day of the outlook.
export function RainCard({
  rain,
  outlook,
}: {
  rain: TodayRain[];
  outlook: TodayOutlookDay[] | null;
}) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const amounts = rain.map((hour) => hour.precipitation ?? 0);
  const total = amounts.reduce((sum, mm) => sum + mm, 0);
  const byAmount = total > 0;
  const hasChance = rain.some((hour) => hour.probability !== null);
  const chances = rain.map((hour) => hour.probability ?? 0);
  const likeliest = Math.max(0, ...chances);
  const scale = Math.max(RAIN_SCALE_MM, ...amounts);
  const wet = byAmount || likeliest >= RAIN_DRY_CHANCE;
  let lead: ReactNode;
  if (byAmount) {
    const heaviest = rain[amounts.indexOf(Math.max(...amounts))].time;
    lead = (
      <Lead
        detail={t("today.rain.heaviest", { time: heaviest })}
        line={
          hasChance
            ? t("today.rain.likely", { percent: Math.round(likeliest) })
            : t("today.rain.inAll")
        }
        picture={<MetricIcon className="text-rain" kind="rain" size={40} />}
        testId="rain-summary"
        unit={t("today.rain.mm")}
        value={oneDecimal(total, lang)}
      />
    );
  } else if (wet) {
    lead = (
      <Lead
        detail={t("today.rain.likeliest", {
          time: rain[chances.indexOf(likeliest)].time,
        })}
        line={t("today.rain.chanceOf")}
        picture={<MetricIcon className="text-rain" kind="rain" size={40} />}
        testId="rain-summary"
        unit={t("today.rain.percentUnit")}
        value={String(Math.round(likeliest))}
      />
    );
  } else {
    const next = outlook?.find(
      (day) => (day.precipitationProbability ?? 0) >= NEXT_RAIN_CHANCE,
    );
    lead = (
      <Lead
        detail={
          next === undefined
            ? null
            : t("today.rain.next", {
                day: weekday(next.date, lang),
                percent: Math.round(next.precipitationProbability!),
              })
        }
        line={t("today.rain.dry")}
        picture={<WeatherIcon condition="overcast" size={40} />}
        small
        testId="rain-summary"
        value={t("today.rain.dryShort")}
      />
    );
  }
  // A dry card has no hours worth bars: the days ahead and their chances.
  if (!wet && outlook !== null) {
    return (
      <div className="flex h-full flex-col gap-3">
        {lead}
        <RainDays days={outlook} />
      </div>
    );
  }
  return (
    <div className="flex h-full flex-col gap-3">
      {lead}
      <div className="flex flex-1 flex-col">
        <ul
          aria-label={t("today.rain.hours")}
          className="flex min-h-10 flex-1 items-end gap-[2px] border-b border-track"
        >
          {rain.map((hour) => {
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
            // A dry hour is the base line alone, no empty bar.
            const drawn = wet && height > 0;
            return (
              <li
                aria-label={label}
                className="flex h-full flex-1 items-end"
                key={hour.time}
                title={label}
              >
                {drawn && (
                  <span
                    className="w-full rounded-t-[2px] bg-rain"
                    style={{
                      height: `${Math.max(8, Math.min(100, height))}%`,
                      opacity: 0.35 + 0.65 * Math.min(1, chance / 100),
                    }}
                  />
                )}
              </li>
            );
          })}
        </ul>
        <div aria-hidden="true" className="mt-1 flex gap-[2px]">
          {rain.map((hour, i) => (
            <span
              className="h-3.5 flex-1 overflow-visible text-[10px] leading-3.5 whitespace-nowrap text-muted"
              key={hour.time}
            >
              {i % 3 === 0 ? hour.time.slice(0, 2) : ""}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

// RainDays is the chance of rain on the days ahead as bars, for a dry card.
function RainDays({ days }: { days: TodayOutlookDay[] }) {
  const { t, i18n } = useTranslation();
  const columns = {
    gridTemplateColumns: `repeat(${days.length}, minmax(0, 1fr))`,
  };
  return (
    <div className="flex flex-1 flex-col">
      <ul
        aria-label={t("today.rain.days")}
        className="grid min-h-12 flex-1 gap-1.5 border-b border-track"
        style={columns}
      >
        {days.map((day) => {
          const chance = day.precipitationProbability;
          const label = `${weekday(day.date, i18n.language)} · ${
            chance === null
              ? "–"
              : t("today.percent", { percent: Math.round(chance) })
          }`;
          return (
            <li
              aria-label={label}
              className="flex h-full flex-col items-center justify-end gap-0.5"
              key={day.date}
              title={label}
            >
              <span className="text-[11px] text-rain">
                {chance !== null && chance >= 10
                  ? t("today.percent", { percent: Math.round(chance) })
                  : ""}
              </span>
              <span className="flex w-full max-w-5 flex-1 items-end">
                {chance !== null && chance > 0 && (
                  <span
                    className="w-full rounded-t-[2px] bg-rain"
                    style={{
                      height: `${Math.max(6, Math.min(100, chance))}%`,
                      opacity: 0.35 + 0.65 * Math.min(1, chance / 100),
                    }}
                  />
                )}
              </span>
            </li>
          );
        })}
      </ul>
      <div aria-hidden="true" className="mt-1 grid gap-1.5" style={columns}>
        {days.map((day) => (
          <span
            className="text-center text-[11px] leading-3.5 text-muted"
            key={day.date}
          >
            {weekday(day.date, i18n.language)}
          </span>
        ))}
      </div>
    </div>
  );
}

// TEMP_COLD and TEMP_WARM are where a range's color is all cold blue and
// all warm gold.
const TEMP_COLD = -10;
const TEMP_WARM = 30;

// tempColor is a temperature's color between cold blue and warm gold.
export function tempColor(celsius: number): string {
  const share = Math.round(
    Math.min(1, Math.max(0, (celsius - TEMP_COLD) / (TEMP_WARM - TEMP_COLD))) *
      100,
  );
  return `color-mix(in oklab, var(--color-sun) ${share}%, var(--color-cold))`;
}

// OutlookCard is the days after the shown one as columns, like the hours on
// the first card: weekday, sky, chance of rain, and the range as an upright
// bar on a scale shared by all the days, cold to warm.
export function OutlookCard({ days }: { days: TodayOutlookDay[] }) {
  const { t, i18n } = useTranslation();
  const lowest = Math.min(...days.map((day) => day.low));
  const highest = Math.max(...days.map((day) => day.high));
  const span = Math.max(1, highest - lowest);
  return (
    <ul
      aria-label={t("today.cards.outlook")}
      className="grid h-full gap-0.5 text-center"
      style={{ gridTemplateColumns: `repeat(${days.length}, minmax(0, 1fr))` }}
    >
      {days.map((day) => {
        const condition = conditionFor(day.code);
        const rain = day.precipitationProbability;
        return (
          <li
            className="flex min-w-0 flex-col items-center gap-[3px]"
            key={day.date}
          >
            <span className="text-[12px] text-muted">
              {weekday(day.date, i18n.language)}
            </span>
            <WeatherIcon
              condition={condition}
              label={t(conditionLabelKey(condition, false))}
              size={22}
            />
            <span className="h-3.5 text-[11px] leading-3.5 text-rain">
              {rain !== null && rain >= 10
                ? t("today.percent", { percent: Math.round(rain) })
                : ""}
            </span>
            <span className="text-[13px] text-ink">
              {t("today.temperature", { value: degrees(day.high) })}
            </span>
            <span
              aria-hidden="true"
              className="relative min-h-7 w-1.5 flex-1 rounded-full bg-track"
            >
              <span
                className="absolute inset-x-0 rounded-full"
                data-testid="day-range-bar"
                style={{
                  top: `${((highest - day.high) / span) * 100}%`,
                  bottom: `${((day.low - lowest) / span) * 100}%`,
                  background: `linear-gradient(to top, ${tempColor(day.low)}, ${tempColor(day.high)})`,
                }}
              />
            </span>
            <span className="text-[12px] text-muted">
              {t("today.temperature", { value: degrees(day.low) })}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

type AirLevel = Exclude<TodayAir["level"], "">;

// AIR_BANDS are the European index's bands from their upper bound (EEA);
// the last is open.
const AIR_BANDS: { level: AirLevel; upTo: number }[] = [
  { level: "good", upTo: 20 },
  { level: "fair", upTo: 40 },
  { level: "moderate", upTo: 60 },
  { level: "poor", upTo: 80 },
  { level: "veryPoor", upTo: 100 },
  { level: "extremelyPoor", upTo: Infinity },
];

// Class names stay whole for Tailwind to find them.
const AIR_TEXT: Record<AirLevel, string> = {
  good: "text-aq-good",
  fair: "text-aq-fair",
  moderate: "text-aq-moderate",
  poor: "text-aq-poor",
  veryPoor: "text-aq-very-poor",
  extremelyPoor: "text-aq-extreme",
};

const POLLEN_BG: Record<TodayPollen["level"], string> = {
  low: "bg-aq-fair",
  moderate: "bg-aq-moderate",
  high: "bg-aq-poor",
  veryHigh: "bg-aq-very-poor",
};

const POLLEN_STEPS: Record<TodayPollen["level"], number> = {
  low: 1,
  moderate: 2,
  high: 3,
  veryHigh: 4,
};

type Point = { x: number; y: number };

// airCurve lays the air's hours out in a width × height box: runs of points
// (an hour without an index breaks the line), the scale's top (the upper
// bound of the peak's band, the peak itself past the last), the band lines
// below it, and the peak.
export function airCurve(hours: TodayAirHour[], width: number, height: number) {
  let peak: { time: string; aqi: number } | null = null;
  for (const h of hours) {
    if (h.aqi !== null && (peak === null || h.aqi > peak.aqi)) {
      peak = { time: h.time, aqi: h.aqi };
    }
  }
  const highest = peak?.aqi ?? 0;
  const bound = AIR_BANDS.find((band) => highest <= band.upTo)!.upTo;
  const top = Number.isFinite(bound) ? bound : highest;
  const lines = AIR_BANDS.map((band) => band.upTo).filter((v) => v < top);
  const y = (aqi: number) => height - 2 - (aqi / top) * (height - 4);
  const step = hours.length > 1 ? width / (hours.length - 1) : 0;
  const runs: Point[][] = [];
  let run: Point[] = [];
  hours.forEach((h, i) => {
    if (h.aqi === null) {
      if (run.length > 0) runs.push(run);
      run = [];
      return;
    }
    run.push({ x: i * step, y: y(h.aqi) });
  });
  if (run.length > 0) runs.push(run);
  return { runs, top, lines, peak, y };
}

// untilMidnight is the hours before the clock first turns back: the ones
// still on the first day.
export function untilMidnight(hours: TodayAirHour[]): TodayAirHour[] {
  const end = hours.findIndex((h, i) => i > 0 && h.time < hours[i - 1].time);
  return end < 0 ? hours : hours.slice(0, end);
}

const CURVE_W = 264;
const CURVE_H = 36;

// AirCurve draws the index over the hours, each stretch in its band's color,
// the band lines dashed, the area under it in the band of now.
function AirCurve({
  hours,
  curve,
  level,
}: {
  hours: TodayAirHour[];
  curve: ReturnType<typeof airCurve>;
  level: AirLevel;
}) {
  const { t } = useTranslation();
  const id = useId().replace(/[^a-zA-Z0-9_-]/g, "");
  const { runs, top, lines, y } = curve;
  const points = (run: Point[]) =>
    run.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ");
  const bands = AIR_BANDS.map((band, i) => ({
    ...band,
    from: i === 0 ? 0 : AIR_BANDS[i - 1].upTo,
  })).filter((band) => band.from < top);
  const labels = hours
    .map((h, i) => ({ time: h.time, i }))
    .filter(({ i }) => i % 6 === 0 && i < hours.length - 2);
  return (
    <div className="flex flex-col gap-1">
      <svg
        aria-label={t("today.air.curve")}
        className="block w-full"
        height={CURVE_H}
        preserveAspectRatio="none"
        role="img"
        viewBox={`0 0 ${CURVE_W} ${CURVE_H}`}
      >
        <defs>
          {bands.map((band) => (
            <clipPath id={`${id}-${band.level}`} key={band.level}>
              <rect
                height={y(band.from) - y(Math.min(band.upTo, top)) + 0.01}
                width={CURVE_W}
                x="0"
                y={y(Math.min(band.upTo, top))}
              />
            </clipPath>
          ))}
        </defs>
        {runs.map((run, i) => (
          <path
            className={AIR_TEXT[level]}
            d={`M${run[0].x},${CURVE_H} L${points(run)} L${run[run.length - 1].x},${CURVE_H} Z`}
            fill="currentColor"
            fillOpacity="0.08"
            key={`area-${i}`}
          />
        ))}
        {lines.map((v) => (
          <line
            className="stroke-border-strong"
            key={v}
            strokeDasharray="2 4"
            strokeWidth="1"
            vectorEffect="non-scaling-stroke"
            x1="0"
            x2={CURVE_W}
            y1={y(v)}
            y2={y(v)}
          />
        ))}
        {bands.map((band) =>
          runs.map((run, i) => (
            <polyline
              className={AIR_TEXT[band.level]}
              clipPath={`url(#${id}-${band.level})`}
              fill="none"
              key={`${band.level}-${i}`}
              points={points(run)}
              stroke="currentColor"
              strokeLinejoin="round"
              strokeWidth="1.8"
              vectorEffect="non-scaling-stroke"
            />
          )),
        )}
      </svg>
      <div aria-hidden="true" className="relative h-3.5 text-[10px] text-muted">
        {labels.map(({ time, i }) => (
          <span
            className="absolute top-0 leading-3.5"
            key={time}
            style={{
              left: `${(i / (hours.length - 1)) * 100}%`,
              transform: i === 0 ? undefined : "translateX(-50%)",
            }}
          >
            {time.slice(0, 2)}
          </span>
        ))}
      </div>
    </div>
  );
}

// AqiRing is the index as a ring filled to its share of 100, in its band's
// color, with a leaf inside.
function AqiRing({ aqi, level }: { aqi: number | null; level: AirLevel | "" }) {
  const circumference = 2 * Math.PI * 16;
  const share = aqi === null ? 0 : Math.min(1, aqi / 100);
  const color = level === "" ? "text-ink-dim" : AIR_TEXT[level];
  return (
    <svg
      aria-hidden="true"
      className={`shrink-0 ${color}`}
      fill="none"
      height="40"
      viewBox="0 0 40 40"
      width="40"
    >
      <circle
        className="stroke-track"
        cx="20"
        cy="20"
        r="16"
        strokeWidth="3.5"
      />
      {share > 0 && (
        <circle
          cx="20"
          cy="20"
          r="16"
          stroke="currentColor"
          strokeDasharray={`${(share * circumference).toFixed(1)} ${circumference.toFixed(1)}`}
          strokeLinecap="round"
          strokeWidth="3.5"
          transform="rotate(-90 20 20)"
        />
      )}
      <path
        d="M14 25c6 0 10-4 10-10-6 0-10 4-10 10zM14 25l5-5"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="1.6"
      />
    </svg>
  );
}

// AirCard is the air quality index with its band, its curve over the hours
// and the pollen worth a load class; tomorrow the index is the day's
// highest.
export function AirCard({ air }: { air: TodayAir }) {
  const { t } = useTranslation();
  const curve = airCurve(air.hours, CURVE_W, CURVE_H);
  const drawn = curve.runs.some((run) => run.length > 1);
  const curveLevel: AirLevel =
    air.level === "" ? "good" : (air.level as AirLevel);
  let detail: string | null = null;
  if (air.aqi !== null) {
    // Today's curve runs 24 h from now, into tomorrow: the peak it names
    // under today's heading is today's, before the clock turns over.
    const peak = air.max
      ? curve.peak
      : airCurve(untilMidnight(air.hours), 1, 1).peak;
    if (peak !== null && !air.max && peak.aqi > Math.round(air.aqi)) {
      detail = t("today.air.upTo", {
        aqi: Math.round(peak.aqi),
        time: peak.time,
      });
    } else if (
      peak !== null &&
      air.max &&
      Math.round(peak.aqi) === Math.round(air.aqi)
    ) {
      detail = t("today.air.worstAt", { time: peak.time });
    }
  }
  const line =
    air.level === ""
      ? t(air.max ? "today.air.qualityMax" : "today.air.quality")
      : t(air.max ? "today.air.summaryMax" : "today.air.summary", {
          level: t(`today.air.levels.${air.level}`),
        });
  return (
    <div className="flex h-full flex-col justify-between gap-2.5">
      {air.aqi !== null && (
        <Lead
          detail={detail}
          line={line}
          lineClass={`font-medium ${air.level === "" ? "text-ink-soft" : AIR_TEXT[air.level]}`}
          picture={<AqiRing aqi={air.aqi} level={air.level} />}
          testId="air-quality"
          value={String(Math.round(air.aqi))}
        />
      )}
      {drawn && <AirCurve curve={curve} hours={air.hours} level={curveLevel} />}
      {air.pollen === null ? null : air.pollen.length === 0 ? (
        <p className="flex items-center gap-1.5 self-start rounded-full bg-raised px-2.5 py-0.5 text-[12px] text-muted">
          <MetricIcon className="text-muted" kind="leaf" size={12} />
          {t("today.air.noPollen")}
        </p>
      ) : (
        <ul
          aria-label={t("today.air.pollen")}
          className="flex flex-wrap gap-1.5"
        >
          {air.pollen.map((pollen) => {
            const steps = POLLEN_STEPS[pollen.level];
            return (
              <li
                className="flex items-center gap-1.5 rounded-full bg-raised px-2.5 py-0.5 text-[12px] text-ink-soft"
                key={pollen.kind}
              >
                <span aria-hidden="true" className="flex gap-0.5">
                  {[1, 2, 3, 4].map((step) => (
                    <span
                      className={`h-1 w-1.5 rounded-full ${
                        step > steps ? "bg-track" : POLLEN_BG[pollen.level]
                      }`}
                      key={step}
                    />
                  ))}
                </span>
                {t(`today.air.kinds.${pollen.kind}`)}{" "}
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
