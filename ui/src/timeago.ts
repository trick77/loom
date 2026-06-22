/**
 * Format an ISO timestamp as a short relative label, matching the Threads list:
 * "just now", "3 minutes ago", "20 hours ago", "yesterday", "4 days ago".
 *
 * Thresholds are elapsed-time based so the output is deterministic:
 * < 1 min -> "just now", < 1 h -> minutes, < 24 h -> hours,
 * < 48 h -> "yesterday", otherwise whole days.
 */
export function formatTimeAgo(iso: string, now: Date = new Date()): string {
  const then = new Date(iso);
  const ms = now.getTime() - then.getTime();
  if (Number.isNaN(ms)) return "";
  if (ms < 0) return "just now";

  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;

  if (ms < minute) return "just now";
  if (ms < hour) return plural(Math.floor(ms / minute), "minute");
  if (ms < day) return plural(Math.floor(ms / hour), "hour");
  if (ms < 2 * day) return "yesterday";
  return plural(Math.floor(ms / day), "day");
}

function plural(value: number, unit: string): string {
  return `${value} ${unit}${value === 1 ? "" : "s"} ago`;
}
