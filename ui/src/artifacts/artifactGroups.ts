import type { Artifact, ArtifactSummary, ArtifactSummaryMonth } from "../api";

// The Artifacts page shows the last RECENT_DAYS by default; anything older is
// reached through search or a period (a year or a month).
export const RECENT_DAYS = 30;
// "This week" starts at local midnight WEEK_DAYS ago, the way the recent window
// starts RECENT_DAYS ago.
const WEEK_DAYS = 7;

export type ArtifactPeriod =
  | { kind: "recent" }
  | { kind: "year"; year: number }
  // month is 1-12.
  | { kind: "month"; year: number; month: number };

// recentWindowStart is local midnight RECENT_DAYS ago. Built from calendar
// fields, not milliseconds, so a DST switch cannot shift it off midnight.
export function recentWindowStart(now: Date): Date {
  return new Date(
    now.getFullYear(),
    now.getMonth(),
    now.getDate() - RECENT_DAYS,
  );
}

// sameLocalDay reports whether two instants fall on the same local calendar day.
export function sameLocalDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

// periodBounds is a year's or month's local [since, until); null for the recent
// window, whose start depends on the clock (recentWindowStart).
export function periodBounds(
  period: ArtifactPeriod,
): { since: Date; until: Date } | null {
  switch (period.kind) {
    case "recent":
      return null;
    case "year":
      return {
        since: new Date(period.year, 0, 1),
        until: new Date(period.year + 1, 0, 1),
      };
    case "month":
      return {
        since: new Date(period.year, period.month - 1, 1),
        until: new Date(period.year, period.month, 1),
      };
  }
}

// periodKey is a primitive identity for a period ("recent", "2025", "2025-03"),
// safe as an effect dependency.
export function periodKey(period: ArtifactPeriod): string {
  switch (period.kind) {
    case "recent":
      return "recent";
    case "year":
      return String(period.year);
    case "month":
      return monthKey(period.year, period.month);
  }
}

function monthKey(year: number, month: number): string {
  return `${year}-${String(month).padStart(2, "0")}`;
}

export type SummaryMonth = {
  key: string;
  year: number;
  month: number;
  count: number;
};

// summaryMonths parses the summary's "YYYY-MM" keys by hand: new Date("2026-10")
// is UTC midnight and reads as September west of Greenwich.
export function summaryMonths(months: ArtifactSummaryMonth[]): SummaryMonth[] {
  const parsed: SummaryMonth[] = [];
  for (const entry of months) {
    const match = /^(\d{4})-(\d{2})$/.exec(entry.month);
    if (match === null) continue;
    const year = Number(match[1]);
    const month = Number(match[2]);
    if (month < 1 || month > 12) continue;
    parsed.push({ key: entry.month, year, month, count: entry.count });
  }
  return parsed;
}

// summaryYears lists the years that hold artifacts, newest first.
export function summaryYears(months: ArtifactSummaryMonth[]): number[] {
  const years: number[] = [];
  for (const { year } of summaryMonths(months)) {
    if (!years.includes(year)) years.push(year);
  }
  return years.sort((a, b) => b - a);
}

// newestOlderYear is the newest year holding an artifact from before the
// recent window, or null when there is none. The month that contains the
// window start holds both kinds, so it walks the months newest first and skips
// as many artifacts as the window holds: the month where the count runs past
// them holds the newest older one. fallbackYear covers counts that disagree.
export function newestOlderYear(
  summary: ArtifactSummary,
  fallbackYear: number,
): number | null {
  if (summary.olderCount <= 0) return null;
  const months = summaryMonths(summary.months);
  const total = months.reduce((sum, month) => sum + month.count, 0);
  const recent = total - summary.olderCount;
  let seen = 0;
  for (const month of months) {
    seen += month.count;
    if (seen > recent) return month.year;
  }
  return fallbackYear;
}

export type GroupMode =
  // The recent window, come whole in one request: Today, This week, This
  // month, each with its tiles above its rows.
  | "recent"
  // The recent window, come in pages (more than one request holds): the same
  // groups, in arrival order.
  | "recentPages"
  // A year or month period: one group per local calendar month.
  | "months"
  // Search results: one group without a heading.
  | "flat";

// ArtifactRun is a stretch of one group: images as a tile grid or other files
// as rows. Its key is its first item's id, which a later page never changes.
export type ArtifactRun = {
  key: string;
  kind: "tiles" | "rows";
  items: Artifact[];
};

export type ArtifactGroup = {
  key: string;
  // "all" has no heading (search results, undated items in a period view).
  kind: "today" | "week" | "month" | "calendarMonth" | "all";
  // Set for calendarMonth; month is 1-12.
  year?: number;
  month?: number;
  // The recent window come whole shows a group's tiles, then its rows. A
  // page-wise view keeps arrival order, a run per change between images and
  // files, so a later page only appends below what is on screen.
  runs: ArtifactRun[];
  // How many items the group holds as loaded.
  size: number;
};

export function isTileArtifact(artifact: Artifact): boolean {
  return artifact.mimeType.startsWith("image/");
}

function artifactTime(artifact: Artifact): number {
  return new Date(artifact.modifiedAt ?? "").getTime();
}

// appendToRuns adds an item to the last run of its kind if that run is the
// last one, else starts a new run.
function appendToRuns(runs: ArtifactRun[], artifact: Artifact) {
  const kind = isTileArtifact(artifact) ? "tiles" : "rows";
  const last = runs[runs.length - 1] as ArtifactRun | undefined;
  if (last !== undefined && last.kind === kind) last.items.push(artifact);
  else runs.push({ key: artifact.id, kind, items: [artifact] });
}

// groupArtifacts splits items into the page's groups, keeping the server's
// order inside each. It never drops an item: the server owns the window, so an
// item past the last boundary lands in the last group.
export function groupArtifacts(
  items: Artifact[],
  now: Date,
  mode: GroupMode,
): ArtifactGroup[] {
  const groups = new Map<string, ArtifactGroup>();
  const add = (
    key: string,
    base: Omit<ArtifactGroup, "runs" | "size">,
    artifact: Artifact,
  ) => {
    let group = groups.get(key);
    if (group === undefined) {
      group = { ...base, runs: [], size: 0 };
      groups.set(key, group);
    }
    group.size++;
    appendToRuns(group.runs, artifact);
  };

  if (mode === "recent" || mode === "recentPages") {
    const today = new Date(
      now.getFullYear(),
      now.getMonth(),
      now.getDate(),
    ).getTime();
    const weekStart = new Date(
      now.getFullYear(),
      now.getMonth(),
      now.getDate() - WEEK_DAYS,
    ).getTime();
    // Come whole, images first, then the rest: each group gets one tile run
    // above one row run. In pages, a later page would put its tiles above
    // rows already on screen, so it keeps arrival order.
    const ordered =
      mode === "recent"
        ? [
            ...items.filter(isTileArtifact),
            ...items.filter((artifact) => !isTileArtifact(artifact)),
          ]
        : items;
    for (const artifact of ordered) {
      const time = artifactTime(artifact);
      // NaN compares false everywhere, so an undated item counts as oldest.
      const kind =
        time >= today ? "today" : time >= weekStart ? "week" : "month";
      add(kind, { key: kind, kind }, artifact);
    }
    const order = ["today", "week", "month"];
    return order.flatMap((key) => {
      const group = groups.get(key);
      return group === undefined ? [] : [group];
    });
  }

  if (mode === "months") {
    const undated: Artifact[] = [];
    for (const artifact of items) {
      const time = artifactTime(artifact);
      if (Number.isNaN(time)) {
        undated.push(artifact);
        continue;
      }
      const date = new Date(time);
      const year = date.getFullYear();
      const month = date.getMonth() + 1;
      const key = monthKey(year, month);
      add(key, { key, kind: "calendarMonth", year, month }, artifact);
    }
    for (const artifact of undated) {
      add("undated", { key: "undated", kind: "all" }, artifact);
    }
    return [...groups.values()];
  }

  for (const artifact of items) {
    add("all", { key: "all", kind: "all" }, artifact);
  }
  return [...groups.values()];
}
