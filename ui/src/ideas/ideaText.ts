import type { TFunction } from "i18next";

import type { Idea } from "../api";
import { formatTimeAgo } from "../timeago";

const DAY_MS = 24 * 60 * 60 * 1000;

// Field caps in runes, as the server enforces them.
export const TITLE_MAX = 80;
export const REASON_MAX = 500;
export const EXCERPT_MAX = 1200;

/**
 * cutExcerpt makes an answer's plain text an idea's passage: citation
 * markers ([1], [https://…]) go, it is cut to the cap, ending in an
 * ellipsis when cut.
 */
export function cutExcerpt(text: string): string {
  const clean = text
    .replace(/\[(?:\d+(?:\s*,\s*\d+)*|https?:\/\/[^\]\s]+)\]/g, "")
    .replace(/[ \t]+([.,;:!?])/g, "$1")
    .replace(/[ \t]{2,}/g, " ")
    .trim();
  return capExcerpt(clean);
}

/**
 * capExcerpt cuts a passage to the cap, ending in an ellipsis when cut, and
 * changes nothing else: a marked passage is read from the page already
 * clean (code keeps its indentation and brackets).
 */
export function capExcerpt(text: string): string {
  const runes = Array.from(text);
  if (runes.length <= EXCERPT_MAX) return text;
  return `${runes
    .slice(0, EXCERPT_MAX - 1)
    .join("")
    .trimEnd()}…`;
}

/** A 'YYYY-MM-DD' civil date as local midnight (no zone shift). */
export function parseDay(day: string): Date {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(y, (m ?? 1) - 1, d ?? 1);
}

/** Today as 'YYYY-MM-DD' in the browser's zone. */
export function todayDay(now: Date = new Date()): string {
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, "0");
  const d = String(now.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

/** "12 Jan" / "12. Jan." */
export function formatDay(day: string, language: string): string {
  return new Intl.DateTimeFormat(language, {
    day: "numeric",
    month: "short",
  }).format(parseDay(day));
}

/** "Mon 5 Jan" / "Mo., 5. Jan." */
export function formatWeekday(day: string, language: string): string {
  return new Intl.DateTimeFormat(language, {
    weekday: "short",
    day: "numeric",
    month: "short",
  }).format(parseDay(day));
}

/** The 'YYYY-MM-DD' day n days after day (before it when n < 0). */
export function addDays(day: string, n: number): string {
  const d = parseDay(day);
  d.setDate(d.getDate() + n);
  return todayDay(d);
}

/** Whole days from a to b ('YYYY-MM-DD'), negative when b is earlier. */
export function daysBetween(a: string, b: string): number {
  return Math.round((parseDay(b).getTime() - parseDay(a).getTime()) / DAY_MS);
}

/**
 * The follow-up date for a due date, as the server computes it
 * (ideas.DefaultRemindOn): a week before; when that is not after today, the
 * day before; else today. A passed due date gets none.
 */
export function defaultRemindOn(due: string, today: string): string | null {
  if (due < today) return null;
  for (const days of [7, 1]) {
    const day = addDays(due, -days);
    if (day > today) return day;
  }
  return today;
}

/**
 * The age slot of a row: "due date passed" for an open idea past its due
 * date, "open for N weeks" from four weeks on (both flagged warn), else how
 * long ago it was noted (weeks from two weeks on).
 */
export function ideaAge(
  idea: Idea,
  t: TFunction,
  now: Date = new Date(),
): { text: string; warn: boolean } {
  const open = idea.state === "open";
  if (open && idea.dueOn !== null && idea.dueOn < todayDay(now)) {
    return { text: t("ideas.duePassed"), warn: true };
  }
  // A done idea's age is since it was done: the Done tab is in that order.
  const since = !open && idea.doneAt !== null ? idea.doneAt : idea.createdAt;
  const days = Math.floor((now.getTime() - new Date(since).getTime()) / DAY_MS);
  const weeks = Math.floor(days / 7);
  if (open && days >= 28) {
    return { text: t("ideas.openForWeeks", { count: weeks }), warn: true };
  }
  if (days >= 14) {
    return { text: t("ideas.weeksAgo", { count: weeks }), warn: false };
  }
  return { text: formatTimeAgo(since, now), warn: false };
}

/** "Due 12 Jan, follow-up 5 Jan", or one of the two, or null. */
export function ideaDates(
  idea: Pick<Idea, "dueOn" | "remindOn">,
  t: TFunction,
  language: string,
): string | null {
  const due = idea.dueOn === null ? null : formatDay(idea.dueOn, language);
  const remind =
    idea.remindOn === null ? null : formatDay(idea.remindOn, language);
  if (due !== null && remind !== null) {
    return t("ideas.dueAndFollowUp", { due, remind });
  }
  if (due !== null) return t("ideas.dueOnly", { date: due });
  if (remind !== null) return t("ideas.followUpOnly", { date: remind });
  return null;
}

/**
 * The composer draft for "Start a thread about it": the title and the
 * reason, the user's own words. Never the passage: it is outside text (a
 * page an answer quoted) and never goes back to a model, not even through a
 * draft the user sends. It only fills the composer, it never sends.
 */
export function ideaThreadDraft(idea: Idea, t: TFunction): string {
  return [idea.title, t("ideas.draftWhy", { reason: idea.reason })].join(
    "\n\n",
  );
}
