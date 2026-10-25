// Text helpers for schedules: the slot read back in the UI language, run
// dates on the schedule's clock, and compact token and duration figures.

// Fixed per-run limits, shown next to the form; the server enforces them.
export const RUN_TOKEN_LIMIT = 300_000;
export const RUN_MINUTES = 5;
export const PROMPT_MAX_LENGTH = 2000;

type Lang = string;

const SUNDAY = new Date(Date.UTC(2026, 0, 4)); // a Sunday

function weekdayName(day: number, lang: Lang, style: "long" | "short") {
  const date = new Date(SUNDAY.getTime() + day * 86_400_000);
  return new Intl.DateTimeFormat(lang, {
    weekday: style,
    timeZone: "UTC",
  }).format(date);
}

function monthName(month: number, lang: Lang) {
  return new Intl.DateTimeFormat(lang, {
    month: "short",
    timeZone: "UTC",
  }).format(new Date(Date.UTC(2026, month - 1, 1)));
}

// listFormat joins items the way the UI language does ("a, b and c"). The
// target library predates Intl.ListFormat, so it is looked up at runtime.
function listFormat(items: string[], lang: Lang) {
  const ListFormat = (
    Intl as unknown as {
      ListFormat?: new (
        lang: string,
        options: { style: string; type: string },
      ) => { format(items: string[]): string };
    }
  ).ListFormat;
  if (ListFormat === undefined) return items.join(", ");
  return new ListFormat(lang, { style: "long", type: "conjunction" }).format(
    items,
  );
}

// parseField expands one concrete cron field into its values; null for a
// field this reader does not handle ("*" is the whole range).
function parseField(field: string, min: number, max: number): number[] | null {
  const values = new Set<number>();
  for (const item of field.split(",")) {
    const [base, stepText] = item.split("/");
    const step = stepText === undefined ? 1 : Number(stepText);
    if (!Number.isInteger(step) || step < 1) return null;
    let lo = min;
    let hi = max;
    if (base !== "*") {
      const [a, b] = base.split("-");
      lo = Number(a);
      hi = b === undefined ? lo : Number(b);
      if (!Number.isInteger(lo) || !Number.isInteger(hi)) return null;
    }
    for (let v = lo; v <= hi; v += step)
      values.add(max === 6 && v === 7 ? 0 : v);
  }
  return [...values].sort((x, y) => x - y);
}

// describeCron reads a concrete five-field cron back in the UI language for
// the common shapes ("Weekly on Thursday at 14:37"); null for anything else,
// where the caller shows the server's English summary.
export function describeCron(
  cron: string,
  lang: Lang,
  words: {
    daily: string;
    weekdays: string;
    weekly: (day: string) => string;
    days: (days: string) => string;
    monthly: (days: string) => string;
    inMonths: (months: string) => string;
    at: (times: string) => string;
  },
): string | null {
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return null;
  const [minuteField, hourField, domField, monthField, dowField] = parts;
  const minutes = parseField(minuteField, 0, 59);
  const hours = parseField(hourField, 0, 23);
  const doms = parseField(domField, 1, 31);
  const months = parseField(monthField, 1, 12);
  const dows = parseField(dowField, 0, 6);
  if (!minutes || !hours || !doms || !months || !dows) return null;
  if (minutes.length !== 1 || hours.length === 0 || hours.length > 3) {
    return null;
  }
  const times = listFormat(
    hours.map(
      (h) =>
        `${String(h).padStart(2, "0")}:${String(minutes[0]).padStart(2, "0")}`,
    ),
    lang,
  );
  const domStar = domField === "*";
  const dowStar = dowField === "*";
  let days: string;
  if (domStar && dowStar) {
    days = words.daily;
  } else if (domStar && dows.join(",") === "1,2,3,4,5") {
    days = words.weekdays;
  } else if (domStar && dows.length === 1) {
    days = words.weekly(weekdayName(dows[0], lang, "long"));
  } else if (domStar) {
    days = words.days(
      listFormat(
        dows.map((d) => weekdayName(d, lang, "long")),
        lang,
      ),
    );
  } else if (dowStar) {
    days = words.monthly(listFormat(doms.map(String), lang));
  } else {
    return null;
  }
  if (months.length < 12) {
    days +=
      " " + words.inMonths(months.map((m) => monthName(m, lang)).join(", "));
  }
  return `${days} ${words.at(times)}`;
}

// slotText is a schedule's slot in the UI language, the server's English
// summary when the shape is not one describeCron reads.
export function slotText(
  cron: string,
  summary: string,
  lang: Lang,
  t: (key: string, options?: Record<string, string>) => string,
): string {
  return (
    describeCron(cron, lang, {
      daily: t("schedules.slot.daily"),
      weekdays: t("schedules.slot.weekdays"),
      weekly: (day) => t("schedules.slot.weekly", { day }),
      days: (days) => t("schedules.slot.days", { days }),
      monthly: (days) => t("schedules.slot.monthly", { days }),
      inMonths: (months) => t("schedules.slot.inMonths", { months }),
      at: (times) => t("schedules.slot.at", { times }),
    }) ?? summary
  );
}

// formatRunDate is a run time on the schedule's clock: "Thu 29 Oct 14:37".
export function formatRunDate(iso: string, lang: Lang, timeZone?: string) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const options: Intl.DateTimeFormatOptions = {
    weekday: "short",
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  };
  try {
    return new Intl.DateTimeFormat(lang, { ...options, timeZone }).format(date);
  } catch {
    return new Intl.DateTimeFormat(lang, options).format(date);
  }
}

// formatDay is a date without the time: "Fri 23 Oct".
export function formatDay(iso: string, lang: Lang) {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return new Intl.DateTimeFormat(lang, {
    weekday: "short",
    day: "numeric",
    month: "short",
  }).format(date);
}

// formatTokens is a token count at a glance: 950, 41k, 300k, 1.2M.
export function formatTokens(n: number) {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${Math.round(n / 1000)}k`;
  return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
}

// formatDuration is a run's wall clock: "1m 48s", "0m 09s".
export function formatDuration(ms: number) {
  const total = Math.max(0, Math.round(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}m ${String(seconds).padStart(2, "0")}s`;
}
