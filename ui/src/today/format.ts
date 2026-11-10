import type { TFunction } from "i18next";

// formatDay renders the user's "YYYY-MM-DD" as a weekday and date. The parts
// build a local date: new Date("2026-11-09") is UTC midnight, the day before
// anywhere west of Greenwich.
export function formatDay(date: string, language: string): string {
  const [y, m, d] = date.split("-").map(Number);
  if (!y || !m || !d) return date;
  try {
    return new Intl.DateTimeFormat(language, {
      weekday: "long",
      day: "numeric",
      month: "long",
    }).format(new Date(y, m - 1, d));
  } catch {
    return date;
  }
}

// formatDuration renders minutes as "9 h 33 min" or "45 min".
export function formatDuration(minutes: number, t: TFunction): string {
  const total = Math.max(0, Math.round(minutes));
  const h = Math.floor(total / 60);
  const m = total % 60;
  if (h === 0) return t("today.sun.minutes", { m });
  return m === 0
    ? t("today.sun.wholeHours", { h })
    : t("today.sun.hours", { h, m });
}

// degrees rounds a temperature for display.
export function degrees(value: number): number {
  return Math.round(value);
}
