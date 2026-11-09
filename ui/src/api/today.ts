import { AuthExpiredError, requestJSON, UserFacingError } from "./http";
import { browserTimezone } from "./schedules";

export type TodayLocation = {
  name: string;
  region?: string;
  country?: string;
  /** home: where the user lives; current: a stay somewhere else. */
  kind: "home" | "current";
  since?: string;
  until?: string;
};

export type TodaySun = {
  /** "HH:MM" in the user's zone; null on a polar day or night. */
  rise: string | null;
  set: string | null;
  dayMinutes: number;
  /** Day length change against the day before the shown one, in minutes. */
  deltaMinutes: number;
  polar: "" | "day" | "night";
  /** Position between rise (0) and set (1) now; <0 before, >1 after; null
   * when the shown day is tomorrow. */
  progress: number | null;
};

export type TodayHour = {
  time: string;
  temperature: number;
  /** WMO weather interpretation code. */
  code: number;
  precipitationProbability: number | null;
};

export type TodayWeatherDay = {
  high: number;
  low: number;
  code: number;
  precipitationProbability: number | null;
};

/** The shown day in numbers: today the air and wind now, tomorrow the wind
 * at its strongest. Any value may be missing. */
export type TodayDetails = {
  feelsLike: number | null;
  /** % */
  humidity: number | null;
  /** km/h */
  wind: number | null;
  gusts: number | null;
  /** Degrees, where the wind blows from. */
  windDirection: number | null;
  uvMax: number | null;
  /** mm over the day. */
  precipitationSum: number | null;
  sunshineHours: number | null;
};

export type TodayRain = {
  time: string;
  /** mm in the hour. */
  precipitation: number | null;
  /** % */
  probability: number | null;
};

export type TodayOutlookDay = {
  /** "YYYY-MM-DD" */
  date: string;
  high: number;
  low: number;
  code: number;
  precipitationProbability: number | null;
};

export type TodayWeather = {
  /** The sky now; null when the shown day is tomorrow. */
  now: { temperature: number; code: number } | null;
  /** The shown day's range and sky. */
  day: TodayWeatherDay;
  /** Today the next slots, tomorrow its daytime, 3 h apart. */
  hours: TodayHour[];
  /** The tile's further cards; null when the forecast has nothing for one. */
  details: TodayDetails | null;
  /** Today the next 12 hours, tomorrow its daytime, hour by hour. */
  rain: TodayRain[] | null;
  /** The days after the shown one. */
  outlook: TodayOutlookDay[] | null;
  /** The weather data licence asks for this wherever the data shows. */
  attribution: { text: string; url: string };
};

export type TodayPollen = {
  kind: "alder" | "birch" | "grass" | "mugwort" | "ragweed";
  level: "low" | "moderate" | "high" | "veryHigh";
};

/** The air on the shown day: today the index now, tomorrow its highest. */
export type TodayAir = {
  /** European air quality index. */
  aqi: number | null;
  level:
    "" | "good" | "fair" | "moderate" | "poor" | "veryPoor" | "extremelyPoor";
  /** True when aqi is the day's highest (tomorrow). */
  max: boolean;
  /** Only the kinds worth a load class; empty: hardly any; null: unknown
   * here (outside Europe, out of season). */
  pollen: TodayPollen[] | null;
  /** The index hour by hour for the curve: today 24 h from now, tomorrow
   * its shown hours as far as the air reaches; null is a gap. */
  hours: TodayAirHour[];
};

export type TodayAirHour = { time: string; aqi: number | null };

const POLLEN_KINDS: readonly string[] = [
  "alder",
  "birch",
  "grass",
  "mugwort",
  "ragweed",
];
const POLLEN_LEVELS: readonly string[] = [
  "low",
  "moderate",
  "high",
  "veryHigh",
];
const AIR_LEVELS: readonly string[] = [
  "good",
  "fair",
  "moderate",
  "poor",
  "veryPoor",
  "extremelyPoor",
];

export type TodayWarning = {
  kind: "rain" | "snow" | "storm" | "frost";
  /** "HH:MM" */
  from: string;
  /** The day `from` falls on: today's next hours can reach into tomorrow. */
  day: "today" | "tomorrow";
  /** Precipitation chance at `from` in percent; null for frost. */
  chance: number | null;
};

export type TodayRun = {
  /** The notification id the run is read by. */
  id: string;
  threadId: string | null;
  title: string;
  body: string;
  createdAt: string;
};

export type Today = {
  /** The shown day, "YYYY-MM-DD": today, or tomorrow from 20:00. */
  date: string;
  day: "today" | "tomorrow";
  location: TodayLocation | null;
  askLocation: boolean;
  sun: TodaySun | null;
  weather: TodayWeather | null;
  air: TodayAir | null;
  /** The shown day's warning: tomorrow's covers its daytime only. */
  warning: TodayWarning | null;
  /** What comes tonight, before midnight; the server sends it only
   * while the shown day is tomorrow. */
  tonight: TodayWarning | null;
  /** Unread schedule runs, newest first. */
  runs: TodayRun[];
  /** RFC3339: when the shown day changes next (20:00 or midnight). */
  refreshAt: string | null;
};

// PlaceNotFoundError is a place the server could not find on the map.
export class PlaceNotFoundError extends Error {
  constructor() {
    super("place not found");
    this.name = "PlaceNotFoundError";
  }
}

// getToday loads the start page's "Today" (from 20:00 the server shows
// tomorrow).
export async function getToday(): Promise<Today> {
  const raw = await requestJSON<Partial<Today> | null>(
    `/api/today`,
    "failed to load today",
    { headers: { "X-Loom-Timezone": browserTimezone() } },
  );
  return normalizeToday(raw);
}

// normalizeWeather takes the forecast only with the shown day's range; the
// earlier `today` key stands in for `day`.
function normalizeWeather(raw: unknown): TodayWeather | null {
  if (raw === null || typeof raw !== "object") return null;
  const weather = raw as Partial<TodayWeather> & { today?: TodayWeatherDay };
  const day = weather.day ?? weather.today;
  if (!day) return null;
  return {
    now: weather.now ?? null,
    day,
    hours: Array.isArray(weather.hours) ? weather.hours : [],
    details: weather.details ?? null,
    rain: nonEmpty(weather.rain),
    outlook: nonEmpty(weather.outlook),
    attribution: weather.attribution ?? { text: "", url: "" },
  };
}

// normalizeAir keeps the air only with something to show; an older server
// sends none.
function normalizeAir(raw: unknown): TodayAir | null {
  if (raw === null || typeof raw !== "object") return null;
  const air = raw as Partial<TodayAir>;
  const aqi = typeof air.aqi === "number" ? air.aqi : null;
  // Kinds and levels this page has no words for are left out: a newer or
  // other server must not show raw keys.
  // A list left empty by the filter is unknown, not "hardly any": the
  // server named a load this page cannot.
  const known = Array.isArray(air.pollen)
    ? air.pollen.filter(
        (p) =>
          POLLEN_KINDS.includes(p?.kind) && POLLEN_LEVELS.includes(p?.level),
      )
    : null;
  const pollen =
    known !== null && known.length === 0 && air.pollen!.length > 0
      ? null
      : known;
  if (aqi === null && (pollen === null || pollen.length === 0)) return null;
  return {
    aqi,
    level:
      typeof air.level === "string" && AIR_LEVELS.includes(air.level)
        ? air.level
        : "",
    max: air.max === true,
    pollen,
    hours: Array.isArray(air.hours)
      ? air.hours
          .filter((h) => typeof h?.time === "string")
          .map((h) => ({
            time: h.time,
            aqi: typeof h.aqi === "number" ? h.aqi : null,
          }))
      : [],
  };
}

function tonightOf(warning: TodayWarning | null): TodayWarning | null {
  return warning !== null && warning.day === "today" ? warning : null;
}

function nonEmpty<T>(list: T[] | null | undefined): T[] | null {
  return Array.isArray(list) && list.length > 0 ? list : null;
}

const WARNING_KINDS: readonly string[] = ["rain", "snow", "storm", "frost"];

// normalizeWarning gives a warning its day (fallback when the answer names
// none) and its chance (null when unknown). A kind this page has no words
// for is no warning: a newer server must not show raw keys.
function normalizeWarning(
  raw: Partial<TodayWarning> | null | undefined,
  fallback: TodayWarning["day"],
): TodayWarning | null {
  if (!raw || typeof raw !== "object" || !raw.from) return null;
  if (typeof raw.kind !== "string" || !WARNING_KINDS.includes(raw.kind)) {
    return null;
  }
  return {
    kind: raw.kind,
    from: raw.from,
    day: raw.day === "today" || raw.day === "tomorrow" ? raw.day : fallback,
    chance: typeof raw.chance === "number" ? raw.chance : null,
  };
}

// normalizeToday fills what an answer leaves out with "nothing to show", so a
// short or older answer hides the section instead of breaking the page. The
// shown day is today unless the answer says tomorrow.
export function normalizeToday(raw: Partial<Today> | null): Today {
  const today = raw ?? {};
  const day = today.day === "tomorrow" ? "tomorrow" : "today";
  return {
    date: typeof today.date === "string" ? today.date : "",
    day,
    location: today.location ?? null,
    askLocation: today.askLocation === true,
    sun: today.sun
      ? { ...today.sun, progress: today.sun.progress ?? null }
      : null,
    weather: normalizeWeather(today.weather),
    air: normalizeAir(today.air),
    // A warning without its day is for the shown day, tonight's for today.
    warning: normalizeWarning(today.warning, day),
    // An older server has no tonight: no pill. Whether it shows is the
    // page's call (only under tomorrow's heading). Tonight ends at
    // midnight: an older server's small hours fall while everyone sleeps.
    tonight: tonightOf(normalizeWarning(today.tonight, "today")),
    runs: Array.isArray(today.runs) ? today.runs : [],
    refreshAt: typeof today.refreshAt === "string" ? today.refreshAt : null,
  };
}

// setLocation stores where the user lives. The server keeps it as a memory.
export async function setLocation(
  place: string,
): Promise<{ place: TodayLocation }> {
  const response = await fetch(`/api/me/location`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ place }),
  });
  if (response.status === 401) throw new AuthExpiredError();
  if (!response.ok) {
    const data = (await response.json().catch(() => ({}))) as {
      error?: string;
      code?: string;
    };
    if (data.code === "not_found") throw new PlaceNotFoundError();
    if (
      (response.status === 400 ||
        response.status === 409 ||
        response.status === 503) &&
      data.error
    ) {
      throw new UserFacingError(data.error);
    }
    throw new Error("failed to save the place");
  }
  return response.json() as Promise<{ place: TodayLocation }>;
}
