import { expect, test } from "vitest";

import i18n from "../i18n";
import type { TodaySun } from "../api/today";
import { degrees, formatDay, formatDuration, safeURL } from "./format";
import { sunPoint } from "./SunTile";
import {
  conditionFor,
  conditionLabelKey,
  isNightAt,
  isNightNow,
  warned,
  warningKey,
} from "./weatherCodes";

const sun: TodaySun = {
  rise: "07:25",
  set: "16:58",
  dayMinutes: 573,
  deltaMinutes: -2,
  polar: "",
  progress: 0.4,
};

test("conditionFor groups the WMO codes", () => {
  expect(conditionFor(0)).toBe("clear");
  expect([1, 2].map(conditionFor)).toEqual(["partly", "partly"]);
  expect(conditionFor(3)).toBe("overcast");
  expect([45, 48].map(conditionFor)).toEqual(["fog", "fog"]);
  for (const code of [51, 61, 67, 80, 82]) {
    expect(conditionFor(code)).toBe("rain");
  }
  for (const code of [71, 77, 85, 86]) {
    expect(conditionFor(code)).toBe("snow");
  }
  for (const code of [95, 96, 99]) {
    expect(conditionFor(code)).toBe("storm");
  }
  expect(conditionFor(42)).toBe("overcast");
});

test("every condition has a label; a clear night is not sunny", () => {
  expect(conditionLabelKey("clear", false)).toBe("today.conditions.sunny");
  expect(conditionLabelKey("clear", true)).toBe("today.conditions.clear");
  for (const condition of [
    "partly",
    "overcast",
    "fog",
    "rain",
    "snow",
    "storm",
  ] as const) {
    const key = conditionLabelKey(condition, false);
    expect(i18n.exists(key)).toBe(true);
  }
});

test("night is outside sunrise..sunset, or the polar night", () => {
  expect(isNightAt("12:00", sun)).toBe(false);
  expect(isNightAt("06:00", sun)).toBe(true);
  expect(isNightAt("21:00", sun)).toBe(true);
  expect(isNightAt("00:00", sun)).toBe(true);
  expect(isNightAt("12:00", null)).toBe(false);
  expect(isNightAt("12:00", { ...sun, polar: "night" })).toBe(true);
  expect(isNightAt("00:00", { ...sun, polar: "day" })).toBe(false);
  expect(isNightAt("00:00", { ...sun, rise: null })).toBe(false);

  expect(isNightNow(sun)).toBe(false);
  expect(isNightNow({ ...sun, progress: -0.1 })).toBe(true);
  expect(isNightNow({ ...sun, progress: 1.2 })).toBe(true);
  expect(isNightNow({ ...sun, polar: "night" })).toBe(true);
  expect(isNightNow({ ...sun, polar: "day", progress: 2 })).toBe(false);
  expect(isNightNow(null)).toBe(false);
});

test("a warning names its day only where the heading does not", () => {
  const rain = (day: "today" | "tomorrow") =>
    ({ kind: "rain", from: "21:00", day, chance: 90 }) as const;
  expect(warningKey("today", rain("today"))).toBe("today.warning.rain");
  expect(warningKey("today", rain("tomorrow"))).toBe(
    "today.warningTomorrow.rain",
  );
  expect(warningKey("tomorrow", rain("tomorrow"))).toBe("today.warning.rain");
  // Tonight's on tomorrow's tile (a server that still sends it there) is
  // named, never read as tomorrow's.
  expect(warningKey("tomorrow", rain("today"))).toBe(
    "today.warningTonight.rain",
  );
  expect(warningKey("tonight", rain("today"))).toBe(
    "today.warningTonight.rain",
  );
});

test("a warning marks the skies it is about", () => {
  const at = { from: "17:00", day: "today", chance: 80 } as const;
  expect(warned("rain", { kind: "rain", ...at })).toBe(true);
  expect(warned("snow", { kind: "snow", ...at })).toBe(true);
  expect(warned("storm", { kind: "storm", ...at })).toBe(true);
  expect(warned("clear", { kind: "rain", ...at })).toBe(false);
  expect(warned("clear", { kind: "frost", ...at })).toBe(false);
  expect(warned("rain", null)).toBe(false);
});

test("sunPoint walks the arc from rise to set", () => {
  expect(sunPoint(0)).toEqual({ x: 20, y: 86 });
  expect(sunPoint(0.5)).toEqual({ x: 110, y: 12 });
  expect(sunPoint(1)).toEqual({ x: 200, y: 86 });
  expect(sunPoint(-1)).toEqual(sunPoint(0));
  expect(sunPoint(3)).toEqual(sunPoint(1));
});

test("format helpers", () => {
  const t = i18n.getFixedT("en");
  expect(formatDuration(573, t)).toBe("9 h 33 min");
  expect(formatDuration(45, t)).toBe("45 min");
  expect(formatDuration(1440, t)).toBe("24 h");
  expect(formatDuration(-5, t)).toBe("0 min");
  expect(formatDay("2026-11-09", "en")).toBe("Monday, November 9");
  expect(formatDay("2026-11-09", "de")).toBe("Montag, 9. November");
  expect(formatDay("garbage", "en")).toBe("garbage");
  expect(degrees(7.6)).toBe(8);
  expect(safeURL("https://open-meteo.com/")).toBe("https://open-meteo.com/");
  expect(safeURL("javascript:alert(1)")).toBeNull();
});
