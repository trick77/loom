import type { TodaySun, TodayWarning } from "../api/today";

export type Condition =
  "clear" | "partly" | "overcast" | "fog" | "rain" | "snow" | "storm";

// conditionFor groups a WMO weather interpretation code. An unknown code
// reads as overcast: a neutral sky claims nothing the forecast did not say.
export function conditionFor(code: number): Condition {
  if (code === 0) return "clear";
  if (code === 1 || code === 2) return "partly";
  if (code === 3) return "overcast";
  if (code === 45 || code === 48) return "fog";
  if ((code >= 51 && code <= 67) || (code >= 80 && code <= 82)) return "rain";
  if ((code >= 71 && code <= 77) || code === 85 || code === 86) return "snow";
  if (code >= 95 && code <= 99) return "storm";
  return "overcast";
}

// conditionLabelKey is the i18n key of a condition's label. A clear night is
// not sunny.
export function conditionLabelKey(condition: Condition, night: boolean) {
  switch (condition) {
    case "clear":
      return night ? "today.conditions.clear" : "today.conditions.sunny";
    case "partly":
      return "today.conditions.partlyCloudy";
    case "overcast":
      return "today.conditions.cloudy";
    case "fog":
      return "today.conditions.fog";
    case "rain":
      return "today.conditions.rain";
    case "snow":
      return "today.conditions.snow";
    case "storm":
      return "today.conditions.thunderstorm";
  }
}

// isNightAt tells whether "HH:MM" lies outside today's daylight. Zero-padded
// times compare as strings; an hour past midnight sorts before the sunrise.
export function isNightAt(time: string, sun: TodaySun | null): boolean {
  if (sun === null) return false;
  if (sun.polar === "night") return true;
  if (sun.polar === "day") return false;
  if (sun.rise === null || sun.set === null) return false;
  return time < sun.rise || time >= sun.set;
}

// isNightNow reads the sun's position now; without one (tomorrow) it is day.
export function isNightNow(sun: TodaySun | null): boolean {
  if (sun === null) return false;
  if (sun.polar !== "") return sun.polar === "night";
  if (sun.progress === null) return false;
  return sun.progress < 0 || sun.progress > 1;
}

// warningKey is a warning's text under the shown day's heading: the day
// shown goes unsaid, another day is named. Tonight's pill says evening: it
// ends at midnight.
export function warningKey(
  shown: "today" | "tomorrow" | "tonight",
  warning: TodayWarning,
): string {
  let group = "warning";
  if (shown === "tonight") {
    group = "warningTonight";
  } else if (warning.day !== shown) {
    group = shown === "today" ? "warningTomorrow" : "warningTonight";
  }
  return `today.${group}.${warning.kind}`;
}

// warned tells whether a slot's sky is what the warning is about.
export function warned(
  condition: Condition,
  warning: TodayWarning | null,
): boolean {
  if (warning === null) return false;
  if (warning.kind === "rain") return condition === "rain";
  if (warning.kind === "snow") return condition === "snow";
  if (warning.kind === "storm") return condition === "storm";
  return false;
}
