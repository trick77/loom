/**
 * Format an ISO timestamp as a short relative label, matching the Threads list:
 * "just now", "3 minutes ago", "20 hours ago", "yesterday", "4 days ago".
 *
 * Thresholds are elapsed-time based so the output is deterministic:
 * < 1 min -> "just now", < 1 h -> minutes, < 24 h -> hours,
 * < 48 h -> "yesterday", otherwise whole days.
 */
import i18n from "./i18n";

export function formatTimeAgo(iso: string, now: Date = new Date()): string {
  const then = new Date(iso);
  const ms = now.getTime() - then.getTime();
  if (Number.isNaN(ms)) return "";
  if (ms < 0) return i18n.t("timeago.justNow");

  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;

  if (ms < minute) return i18n.t("timeago.justNow");
  if (ms < hour)
    return i18n.t("timeago.minute", { count: Math.floor(ms / minute) });
  if (ms < day) return i18n.t("timeago.hour", { count: Math.floor(ms / hour) });
  if (ms < 2 * day) return i18n.t("timeago.yesterday");
  return i18n.t("timeago.day", { count: Math.floor(ms / day) });
}
