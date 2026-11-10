// greetingVisit remembers the day of a user's last start screen, so the greeting
// can say their name once a day (and welcome them back after a week away)
// instead of at random. Per browser and user: a server-side last-seen would be
// exact across devices, but is not worth a backend change for a greeting. Keyed
// by user id, so on a shared browser one user's visit doesn't spend another's.

const STORAGE_PREFIX = "loom.greeting.lastSeen.";

// The greeting day starts at 05:00, with the morning band: a midnight reset
// would name a night owl at 23:40 and again at 00:10.
const DAY_STARTS_AT_HOUR = 5;

export type GreetingVisit = {
  // No start screen yet today in this browser.
  firstToday: boolean;
  // Whole greeting days since the last start screen; null when unknown.
  awayDays: number | null;
};

// greetingDay is the local calendar date `now` counts as; built through the
// Date constructor so a DST switch never shifts it.
function greetingDay(now: Date): Date {
  return new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate(),
    now.getHours() - DAY_STARTS_AT_HOUR,
    now.getMinutes(),
  );
}

function dayKey(now: Date): string {
  const d = greetingDay(now);
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  return `${d.getFullYear()}-${mm}-${dd}`;
}

// dayNumber counts calendar days via UTC so a DST switch never shifts the count.
function dayNumber(year: number, month: number, day: number): number {
  return Math.round(Date.UTC(year, month, day) / 86_400_000);
}

// readGreetingVisit returns undefined when storage is unavailable (private mode,
// blocked site data): the caller then cannot tell a first visit from a later one.
export function readGreetingVisit(
  userID: string,
  now: Date,
): GreetingVisit | undefined {
  let raw: string | null;
  try {
    raw = localStorage.getItem(STORAGE_PREFIX + userID);
  } catch {
    return undefined;
  }
  const match = raw === null ? null : /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw);
  if (match === null) return { firstToday: true, awayDays: null };
  const last = dayNumber(
    Number(match[1]),
    Number(match[2]) - 1,
    Number(match[3]),
  );
  const today = greetingDay(now);
  const away =
    dayNumber(today.getFullYear(), today.getMonth(), today.getDate()) - last;
  if (away < 0) return { firstToday: true, awayDays: null };
  return { firstToday: away > 0, awayDays: away };
}

export function rememberGreetingVisit(userID: string, now: Date): void {
  try {
    localStorage.setItem(STORAGE_PREFIX + userID, dayKey(now));
  } catch {
    // Private mode or a full quota: the next start screen greets by name again.
  }
}
