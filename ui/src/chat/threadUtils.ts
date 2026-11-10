import i18n from "../i18n";
import type { Message } from "../api";
import type { ComposerAttachment } from "./useDocumentAttachments";
import type { GreetingVisit } from "./greetingVisit";
import type { MessageWithActivityTrace } from "./types";

// Start-screen greetings. The name is a salutation, not decoration: the first
// start screen of the day greets by first name ("Good morning, Jan"), a week away
// earns "Welcome back, Jan", and every later screen that day gets a nameless line
// (a question, a nudge, a short motivational one). A name on a question reads like
// a bot or a reprimand, and a name on every screen stops meaning anything.
//
// `key` indexes the `greetings` i18n namespace. Salutations provide `named` and
// `unnamed` forms, lines only `unnamed`. `when` gates an entry to a moment; an
// entry without one is always eligible.
type Greeting = {
  key: string;
  when?: (now: Date) => boolean;
};

const minutes = (d: Date) => d.getHours() * 60 + d.getMinutes();
// between is a local time-of-day band [from, to) in hours; from > to wraps midnight.
const between = (from: number, to: number) => (d: Date) => {
  const m = minutes(d);
  const lo = from * 60;
  const hi = to * 60;
  return lo <= hi ? m >= lo && m < hi : m >= lo || m < hi;
};
const morning = between(5, 12);
const afternoon = between(12, 18);
const evening = between(18, 23);
const night = between(23, 5);
const onDays =
  (...days: number[]) =>
  (d: Date) =>
    days.includes(d.getDay()); // 0=Sun … 6=Sat
const all =
  (...tests: ((d: Date) => boolean)[]) =>
  (d: Date) =>
    tests.every((t) => t(d));
// onDate matches a calendar day range within one month (month 1-12).
const onDate =
  (month: number, first: number, last = first) =>
  (d: Date) =>
    d.getMonth() + 1 === month && d.getDate() >= first && d.getDate() <= last;

// Holidays beat the time of day on the first greeting.
const HOLIDAYS: Greeting[] = [
  { key: "christmas", when: onDate(12, 24, 26) },
  { key: "newYear", when: onDate(1, 1, 3) },
];

const SALUTATIONS: Greeting[] = [
  { key: "goodMorning", when: morning },
  { key: "goodDay", when: afternoon },
  { key: "goodEvening", when: evening },
  { key: "lateNight", when: night },
  { key: "niceToSeeYou" },
];

const WELCOME_BACK = "welcomeBack";
const WELCOME_BACK_DAYS = 7;

const LINES: Greeting[] = [
  // Generic.
  { key: "whatsOnYourMind" },
  { key: "whereToStart" },
  { key: "whatAreWeWorkingOn" },
  { key: "readyWhenYouAre" },
  { key: "backAtIt" },
  { key: "whatsNew" },
  // Motivational, short on purpose.
  { key: "oneStepAtATime" },
  { key: "smallSteps" },
  { key: "startSmall" },
  { key: "goodIdeas" },
  { key: "curiosityFirst" },
  { key: "makeItCount" },
  // A moment of the day or week.
  { key: "earlyStart", when: between(5, 7.5) },
  { key: "coffeeFirst", when: morning },
  { key: "newDayNewIdeas", when: morning },
  { key: "freshWeek", when: all(onDays(1), morning) },
  { key: "weekIsYours", when: onDays(1) },
  { key: "lunchBreak", when: between(11.5, 13.5) },
  { key: "afternoonSlump", when: between(14, 17) },
  { key: "quittingTime", when: all(onDays(1, 2, 3, 4, 5), between(16, 18)) },
  { key: "finallyFriday", when: all(onDays(5), between(5, 18)) },
  { key: "weekendSoon", when: all(onDays(5), between(14, 23)) },
  { key: "howWasDay", when: evening },
  { key: "quietEvening", when: evening },
  { key: "oneMoreIdea", when: evening },
  { key: "nightOwl", when: night },
  { key: "nightShift", when: night },
  { key: "happyWeekend", when: onDays(0, 6) },
  { key: "sundayProject", when: onDays(0) },
  { key: "lastDayOfYear", when: onDate(12, 31) },
];

// Without storage a first visit can't be told from a later one: name a third of
// the greetings, so it still happens without repeating on every screen.
const NAMED_SHARE_WITHOUT_STORAGE = 1 / 3;

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

const eligible = (pool: Greeting[], now: Date) =>
  pool.filter((g) => g.when === undefined || g.when(now));

// salutationKeys lists the salutations open at `now`, a holiday shutting out the rest.
function salutationKeys(now: Date): string[] {
  const holidays = eligible(HOLIDAYS, now);
  return (holidays.length > 0 ? holidays : eligible(SALUTATIONS, now)).map(
    (g) => g.key,
  );
}

// weightedLine picks a nameless line; one tied to a moment counts double, so the
// moment shows instead of drowning in the always-eligible ones.
function weightedLine(now: Date, rand: () => number): string {
  const pool = eligible(LINES, now);
  const weights = pool.map((g) => (g.when === undefined ? 1 : 2));
  let r = rand() * weights.reduce((a, b) => a + b, 0);
  for (let i = 0; i < pool.length; i += 1) {
    r -= weights[i];
    if (r < 0) return pool[i].key;
  }
  return pool[pool.length - 1].key;
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

// pickGreeting makes the choice once. `visit` is this browser's last start screen
// (greetingVisit); undefined when storage is unavailable. `now`/`rand` are
// injectable so tests can pin the moment and the choice.
export function pickGreeting(
  fullName: string,
  now = new Date(),
  rand = Math.random,
  visit?: GreetingVisit,
): GreetingPick {
  const name = firstName(fullName);
  const form = name === "" ? "unnamed" : "named";
  // A holiday outranks the welcome back: it only comes once a year.
  const away =
    visit !== undefined && (visit.awayDays ?? 0) >= WELCOME_BACK_DAYS;
  if (away && eligible(HOLIDAYS, now).length === 0) {
    return { key: WELCOME_BACK, form, name };
  }
  const salute =
    visit === undefined
      ? rand() < NAMED_SHARE_WITHOUT_STORAGE
      : visit.firstToday;
  if (salute) {
    const keys = salutationKeys(now);
    const key =
      keys[Math.min(Math.floor(rand() * keys.length), keys.length - 1)];
    return { key, form, name };
  }
  return { key: weightedLine(now, rand), form: "unnamed", name };
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
  visit?: GreetingVisit,
): string {
  return greetingTextFor(pickGreeting(fullName, now, rand, visit));
}

// possibleGreetings enumerates every string greetingForNow could return at `now`
// for the given name, whatever the visit — used by tests to assert membership
// without duplicating the pool.
export function possibleGreetings(
  fullName: string,
  now = new Date(),
): string[] {
  const name = firstName(fullName);
  const form = name === "" ? "unnamed" : "named";
  const out = new Set<string>();
  for (const key of [WELCOME_BACK, ...salutationKeys(now)]) {
    out.add(greetingText(key, form, name));
  }
  for (const g of eligible(LINES, now)) {
    out.add(greetingText(g.key, "unnamed", name));
  }
  return [...out];
}

export function isNearBottom(element: HTMLElement): boolean {
  return element.scrollHeight - element.scrollTop - element.clientHeight <= 48;
}

// previousUserMessages is previousUserMessage for every index at once: the
// nearest user message before each position, in one pass.
export function previousUserMessages(messages: Message[]): (Message | null)[] {
  const out = new Array<Message | null>(messages.length);
  let last: Message | null = null;
  for (let i = 0; i < messages.length; i++) {
    out[i] = last;
    if (messages[i].role === "user") last = messages[i];
  }
  return out;
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
