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
  /** Day length change against yesterday, in minutes. */
  deltaMinutes: number;
  polar: "" | "day" | "night";
  /** Position between rise (0) and set (1) now; <0 before, >1 after. */
  progress: number;
};

export type TodayHour = {
  time: string;
  temperature: number;
  /** WMO weather interpretation code. */
  code: number;
  precipitationProbability: number | null;
};

export type TodayWeather = {
  now: { temperature: number; code: number };
  today: {
    high: number;
    low: number;
    code: number;
    precipitationProbability: number | null;
  };
  hours: TodayHour[];
  /** The weather data licence asks for this wherever the data shows. */
  attribution: { text: string; url: string };
};

export type TodayWarning = {
  kind: "rain" | "snow" | "storm" | "frost";
  /** "HH:MM" */
  from: string;
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
  /** The user's date, "YYYY-MM-DD". */
  date: string;
  /** tiles on the first visit of the day, line after. */
  view: "tiles" | "line";
  location: TodayLocation | null;
  askLocation: boolean;
  sun: TodaySun | null;
  weather: TodayWeather | null;
  warning: TodayWarning | null;
  /** Unread schedule runs, newest first. */
  runs: TodayRun[];
};

// PlaceNotFoundError is a place the server could not find on the map.
export class PlaceNotFoundError extends Error {
  constructor() {
    super("place not found");
    this.name = "PlaceNotFoundError";
  }
}

// getToday loads the start page's "Today". compact is for a phone, which shows
// the line anyway: the server then answers "line" and keeps the day's tiles
// for a wider screen.
export async function getToday(
  options: { compact?: boolean } = {},
): Promise<Today> {
  const raw = await requestJSON<Partial<Today> | null>(
    options.compact ? `/api/today?compact=1` : `/api/today`,
    "failed to load today",
    { headers: { "X-Loom-Timezone": browserTimezone() } },
  );
  return normalizeToday(raw);
}

// normalizeToday fills what an answer leaves out with "nothing to show", so a
// short or older answer hides the section instead of breaking the page.
export function normalizeToday(raw: Partial<Today> | null): Today {
  const today = raw ?? {};
  return {
    date: typeof today.date === "string" ? today.date : "",
    view: today.view === "line" ? "line" : "tiles",
    location: today.location ?? null,
    askLocation: today.askLocation === true,
    sun: today.sun ?? null,
    weather: today.weather ?? null,
    warning: today.warning ?? null,
    runs: Array.isArray(today.runs) ? today.runs : [],
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
