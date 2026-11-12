import type { TFunction } from "i18next";

import type { Idea } from "../api";
import { formatTimeAgo } from "../timeago";

const DAY_MS = 24 * 60 * 60 * 1000;

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
