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

export type TodayWeather = {
  /** The sky now; null when the shown day is tomorrow. */
  now: { temperature: number; code: number } | null;
  /** The shown day's range and sky. */
  day: TodayWeatherDay;
  /** Today the next slots, tomorrow its daytime, 3 h apart. */
  hours: TodayHour[];
  /** The weather data licence asks for this wherever the data shows. */
  attribution: { text: string; url: string };
};

export type TodayWarning = {
  kind: "rain" | "snow" | "storm" | "frost";
  /** "HH:MM" */
  from: string;
  /** The day the warning is for: in the evening it can be for tonight. */
  day: "today" | "tomorrow";
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
  /** The shown day, "YYYY-MM-DD": today, or tomorrow from 18:00. */
  date: string;
  day: "today" | "tomorrow";
  location: TodayLocation | null;
  askLocation: boolean;
  sun: TodaySun | null;
  weather: TodayWeather | null;
  warning: TodayWarning | null;
  /** Unread schedule runs, newest first. */
  runs: TodayRun[];
  /** RFC3339: when the shown day changes next (18:00 or midnight). */
  refreshAt: string | null;
};

// PlaceNotFoundError is a place the server could not find on the map.
export class PlaceNotFoundError extends Error {
  constructor() {
    super("place not found");
    this.name = "PlaceNotFoundError";
  }
}

// getToday loads the start page's "Today" (from 18:00 the server shows
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
    attribution: weather.attribution ?? { text: "", url: "" },
  };
}

// normalizeToday fills what an answer leaves out with "nothing to show", so a
// short or older answer hides the section instead of breaking the page. The
// shown day is today unless the answer says tomorrow.
export function normalizeToday(raw: Partial<Today> | null): Today {
  const today = raw ?? {};
  const day = today.day === "tomorrow" ? "tomorrow" : "today";
  const warning = today.warning ?? null;
  return {
    date: typeof today.date === "string" ? today.date : "",
    day,
    location: today.location ?? null,
    askLocation: today.askLocation === true,
    sun: today.sun
      ? { ...today.sun, progress: today.sun.progress ?? null }
      : null,
    weather: normalizeWeather(today.weather),
    // A warning without its day is for the shown day.
    warning:
      warning === null
        ? null
        : {
            ...warning,
            day:
              warning.day === "today" || warning.day === "tomorrow"
                ? warning.day
                : day,
          },
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
