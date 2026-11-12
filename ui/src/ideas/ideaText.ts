import type { TFunction } from "i18next";

import type { Idea, Message } from "../api";
import type { PastedSource } from "../api/types";
import { stripPastedBlocks } from "../chat/pastedText";
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
 * The composer draft for "Start a thread about it": the title, the reason
 * and the question the captured answer replied to, the user's own words, as
 * text. The passage is outside text (a page an answer quoted): it rides along
 * as a separate block marked outside (ideaPassageBlock), so the turn it
 * starts may not act on it. It only fills the composer, it never sends.
 */
export function ideaThreadDraft(idea: Idea, t: TFunction): string {
  const lines = [t("ideas.draftWhy", { reason: idea.reason })];
  if (idea.question.trim() !== "")
    lines.push(t("ideas.draftQuestion", { question: idea.question }));
  return [idea.title, lines.join("\n")].join("\n\n");
}

/**
 * questionBefore is the user's message right before messageId: what that
 * answer replied to, without pasted blocks (what the server keeps with the
 * idea on save). "" when there is none.
 */
export function questionBefore(
  messages: readonly Pick<Message, "id" | "role" | "content" | "pastedTexts">[],
  messageId: string,
): string {
  const at = messages.findIndex((m) => m.id === messageId);
  for (let i = at - 1; i >= 0; i--) {
    const m = messages[i];
    if (m.role !== "user") continue;
    const blocks = m.pastedTexts ?? [];
    if (blocks.length === 0) return m.content.trim();
    const { text, matched } = stripPastedBlocks(m.content, blocks);
    // Fails closed like the server: an idea's passage that could not be
    // taken out must not show as the user's words.
    if (
      blocks.some((block, index) => block.outside === true && !matched[index])
    )
      return "";
    return text.trim();
  }
  return "";
}

/** The idea's passage as a block marked outside text; null without one. */
export function ideaPassageBlock(
  idea: Idea,
): { text: string; source: PastedSource } | null {
  if (idea.excerpt.trim() === "") return null;
  return {
    text: idea.excerpt,
    source: {
      outside: true,
      ideaId: idea.id,
      ideaTitle: idea.title,
      source: idea.threadTitle,
    },
  };
}
