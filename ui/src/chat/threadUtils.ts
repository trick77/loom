import i18n from "../i18n";
import type { Message } from "../api";
import type { ComposerAttachment } from "./useDocumentAttachments";
import type { MessageWithActivityTrace } from "./types";

// A greeting for the start screen. `key` indexes the localized text under the
// `greetings` catalog namespace; `named`/`unnamed` flag which forms that entry
// provides (named forms carry a `{{name}}` placeholder and need a name to
// render), and `when` gates it to a time-of-day or weekday — entries with no
// `when` are always eligible.
type Greeting = {
  key: string;
  named?: boolean;
  unnamed?: boolean;
  when?: (now: Date) => boolean;
};

// Time-of-day bands (non-overlapping) and weekday helpers, keyed on local time.
const morning = (d: Date) => d.getHours() >= 5 && d.getHours() < 12;
const afternoon = (d: Date) => d.getHours() >= 12 && d.getHours() < 18;
const evening = (d: Date) => d.getHours() >= 18 && d.getHours() < 23;
const night = (d: Date) => d.getHours() >= 23 || d.getHours() < 5;
const onDay = (day: number) => (d: Date) => d.getDay() === day; // 0=Sun … 6=Sat
const weekend = (d: Date) => d.getDay() === 0 || d.getDay() === 6;

// The rotating pool. Mirrors the Claude home-screen set (brand lines adapted to
// Loom), folding in the original five greetings so nothing is lost. Text lives in
// the i18n `greetings` namespace so both languages rotate through the same slots.
const GREETINGS: Greeting[] = [
  // Generic — always eligible.
  { key: "returns", named: true },
  { key: "backAtIt", named: true, unnamed: true },
  { key: "heyThere", named: true, unnamed: true },
  { key: "howAreYou", named: true, unnamed: true },
  { key: "howWasDay", named: true, unnamed: true },
  { key: "howsItGoing", named: true, unnamed: true },
  { key: "welcome", named: true, unnamed: true },
  { key: "whatsNew", named: true, unnamed: true },
  // Morning.
  { key: "goodMorning", named: true, unnamed: true, when: morning },
  { key: "morning", named: true, when: morning },
  { key: "coffee", unnamed: true, when: morning },
  // Afternoon.
  { key: "goodAfternoon", named: true, unnamed: true, when: afternoon },
  { key: "afternoon", named: true, when: afternoon },
  // Evening.
  { key: "goodEvening", named: true, unnamed: true, when: evening },
  { key: "evening", named: true, unnamed: true, when: evening },
  // Late night.
  { key: "nightOwl", unnamed: true, when: night },
  { key: "upLate", named: true, when: night },
  // Weekdays.
  { key: "happyMonday", named: true, unnamed: true, when: onDay(1) },
  { key: "happyTuesday", named: true, unnamed: true, when: onDay(2) },
  { key: "happyWednesday", named: true, unnamed: true, when: onDay(3) },
  { key: "happyThursday", named: true, unnamed: true, when: onDay(4) },
  { key: "happyFriday", named: true, unnamed: true, when: onDay(5) },
  { key: "fridayFeeling", named: true, unnamed: true, when: onDay(5) },
  { key: "happySaturday", named: true, unnamed: true, when: onDay(6) },
  { key: "happySunday", named: true, unnamed: true, when: onDay(0) },
  { key: "sundaySession", named: true, unnamed: true, when: onDay(0) },
  // Weekend.
  { key: "weekend", named: true, unnamed: true, when: weekend },
];

function firstName(fullName: string): string {
  const trimmed = fullName.trim();
  return trimmed === "" ? "" : trimmed.split(/\s+/)[0];
}

// greetingText resolves a localized greeting slot from the catalog.
function greetingText(
  key: string,
  form: "named" | "unnamed",
  name: string,
): string {
  return i18n.t(`greetings.${key}.${form}`, { name });
}

// eligibleGreetings returns the pool entries valid at `now`, dropping named-only
// entries when there is no name to render them with.
function eligibleGreetings(name: string, now: Date): Greeting[] {
  return GREETINGS.filter((g) => {
    if (g.when !== undefined && !g.when(now)) return false;
    if (name === "" && !g.unnamed) return false;
    return true;
  });
}

// GreetingPick is the stable, language-independent outcome of choosing a greeting:
// which catalog slot and which form, plus the name to interpolate. Callers memoize
// the pick and translate it at render time (greetingTextFor / t) so the greeting
// re-localizes when the UI language switches instead of freezing at mount.
export type GreetingPick = {
  key: string;
  form: "named" | "unnamed";
  name: string;
};

function chooseForm(
  greeting: Greeting,
  name: string,
  rand: () => number,
): "named" | "unnamed" {
  const canName = name !== "" && Boolean(greeting.named);
  const useNamed = canName && (!greeting.unnamed || rand() < 0.5);
  if (useNamed) return "named";
  if (greeting.unnamed) return "unnamed";
  return "named";
}

// pickGreeting makes the time/day-appropriate random choice once. `now`/`rand` are
// injectable so callers (and tests) can pin the moment and the choice.
export function pickGreeting(
  fullName: string,
  now = new Date(),
  rand = Math.random,
): GreetingPick {
  const name = firstName(fullName);
  const eligible = eligibleGreetings(name, now);
  const pick = eligible[Math.floor(rand() * eligible.length)] ?? eligible[0];
  return { key: pick.key, form: chooseForm(pick, name, rand), name };
}

// greetingTextFor localizes a pick against the current i18n language.
export function greetingTextFor(pick: GreetingPick): string {
  return greetingText(pick.key, pick.form, pick.name);
}

// greetingForNow picks and localizes in one call (kept for tests and any
// non-reactive caller). Reactive UI should memoize pickGreeting and translate at
// render time so a language switch takes effect.
export function greetingForNow(
  fullName: string,
  now = new Date(),
  rand = Math.random,
): string {
  return greetingTextFor(pickGreeting(fullName, now, rand));
}

// possibleGreetings enumerates every string greetingForNow could return at `now`
// for the given name — used by tests to assert membership without duplicating the
// pool.
export function possibleGreetings(
  fullName: string,
  now = new Date(),
): string[] {
  const name = firstName(fullName);
  const out = new Set<string>();
  for (const g of eligibleGreetings(name, now)) {
    if (name !== "" && g.named) out.add(greetingText(g.key, "named", name));
    if (g.unnamed) out.add(greetingText(g.key, "unnamed", name));
  }
  return [...out];
}

export function isNearBottom(element: HTMLElement): boolean {
  return element.scrollHeight - element.scrollTop - element.clientHeight <= 48;
}

export function previousUserMessage(
  messages: Message[],
  beforeIndex: number,
): Message | null {
  for (let index = beforeIndex - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (message.role === "user") return message;
  }
  return null;
}

// reconcileUserMessage folds the server-confirmed user message into the list.
// When the optimistic placeholder identified by `placeholderID` is present it is
// replaced in place — keeping its slot and clientKey so the React key is stable
// (no remount/scroll jump) — and any stray copy of the confirmed id is dropped, so
// a delayed/duplicate user_message event can never leave two bubbles behind. When
// the placeholder is gone but a copy of the confirmed message is already present
// (e.g. a route refresh reloaded it), the list is returned unchanged: that keeps
// the loaded object's richer fields, key and position rather than overwriting them
// with the streamed payload. Otherwise the message is appended once.
export function reconcileUserMessage(
  messages: MessageWithActivityTrace[],
  placeholderID: string | null,
  confirmed: MessageWithActivityTrace,
): MessageWithActivityTrace[] {
  const placeholderIndex =
    placeholderID !== null
      ? messages.findIndex((message) => message.id === placeholderID)
      : -1;
  if (placeholderIndex !== -1) {
    const reconciled: MessageWithActivityTrace = {
      ...confirmed,
      clientKey: messages[placeholderIndex].clientKey,
    };
    const result: MessageWithActivityTrace[] = [];
    messages.forEach((message, index) => {
      if (index === placeholderIndex) result.push(reconciled);
      else if (message.id !== confirmed.id) result.push(message);
    });
    return result;
  }
  if (messages.some((message) => message.id === confirmed.id)) return messages;
  return [...messages, { ...confirmed, clientKey: confirmed.id }];
}

export function updateMessageAttachment(
  messages: MessageWithActivityTrace[],
  attachmentId: string,
  patch: Partial<ComposerAttachment>,
): MessageWithActivityTrace[] {
  return messages.map((message) => {
    if (message.attachments === undefined) return message;
    const attachments = message.attachments.map((attachment) =>
      attachment.id === attachmentId ? { ...attachment, ...patch } : attachment,
    );
    return { ...message, attachments };
  });
}
