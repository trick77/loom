import type { Message } from "../api";
import type { ComposerAttachment } from "./useDocumentAttachments";
import type { MessageWithActivityTrace } from "./types";

// A greeting for the start screen. `named` contains a `{name}` placeholder and is
// only usable when we have a name; `unnamed` is the nameless form. An entry may
// carry both (the renderer picks one), and `when` gates it to a time-of-day or
// weekday — entries with no `when` are always eligible.
type Greeting = { named?: string; unnamed?: string; when?: (now: Date) => boolean };

// Time-of-day bands (non-overlapping) and weekday helpers, keyed on local time.
const morning = (d: Date) => d.getHours() >= 5 && d.getHours() < 12;
const afternoon = (d: Date) => d.getHours() >= 12 && d.getHours() < 18;
const evening = (d: Date) => d.getHours() >= 18 && d.getHours() < 23;
const night = (d: Date) => d.getHours() >= 23 || d.getHours() < 5;
const onDay = (day: number) => (d: Date) => d.getDay() === day; // 0=Sun … 6=Sat
const weekend = (d: Date) => d.getDay() === 0 || d.getDay() === 6;

// The rotating pool. Mirrors the Claude home-screen set (brand lines adapted to
// Loom), folding in the original five greetings so nothing is lost.
const GREETINGS: Greeting[] = [
  // Generic — always eligible.
  { named: "{name} returns!" },
  { named: "Back at it, {name}", unnamed: "Back at it!" },
  { named: "Hey there, {name}", unnamed: "Hey there" },
  { unnamed: "Greetings, whoever you are" },
  { named: "Hi {name}, how are you?", unnamed: "Hi, how are you?" },
  { named: "How was your day, {name}?", unnamed: "How was your day?" },
  { named: "How's it going, {name}?", unnamed: "How's it going?" },
  { named: "Welcome, {name}", unnamed: "Welcome" },
  { named: "What's new, {name}?", unnamed: "What's new?" },
  // Morning.
  { named: "Good morning, {name}", unnamed: "Good morning", when: morning },
  { named: "Morning, {name}", when: morning },
  { unnamed: "Coffee and Loom time?", when: morning },
  // Afternoon.
  { named: "Good afternoon, {name}", unnamed: "Good afternoon", when: afternoon },
  { named: "Afternoon, {name}", when: afternoon },
  // Evening.
  { named: "Good evening, {name}", unnamed: "Good evening", when: evening },
  { named: "Evening, {name}", unnamed: "Evening", when: evening },
  // Late night.
  { unnamed: "Hello, night owl", when: night },
  { named: "Up late, {name}?", when: night },
  // Weekdays.
  { named: "Happy Monday, {name}", unnamed: "Happy Monday", when: onDay(1) },
  { named: "Happy Tuesday, {name}", unnamed: "Happy Tuesday", when: onDay(2) },
  { named: "Happy Wednesday, {name}", unnamed: "Happy Wednesday", when: onDay(3) },
  { named: "Happy Thursday, {name}", unnamed: "Happy Thursday", when: onDay(4) },
  { named: "Happy Friday, {name}", unnamed: "Happy Friday", when: onDay(5) },
  { named: "That Friday feeling, {name}", unnamed: "That Friday feeling", when: onDay(5) },
  { named: "Happy Saturday, {name}", unnamed: "Happy Saturday!", when: onDay(6) },
  { named: "Happy Sunday, {name}", unnamed: "Happy Sunday", when: onDay(0) },
  { named: "Sunday session, {name}?", unnamed: "Sunday session?", when: onDay(0) },
  // Weekend.
  { named: "Welcome to the weekend, {name}", unnamed: "Welcome to the weekend", when: weekend },
];

function firstName(fullName: string): string {
  const trimmed = fullName.trim();
  return trimmed === "" ? "" : trimmed.split(/\s+/)[0];
}

// eligibleGreetings returns the pool entries valid at `now`, dropping named-only
// entries when there is no name to render them with.
function eligibleGreetings(name: string, now: Date): Greeting[] {
  return GREETINGS.filter((g) => {
    if (g.when !== undefined && !g.when(now)) return false;
    if (name === "" && g.unnamed === undefined) return false;
    return true;
  });
}

function renderGreeting(greeting: Greeting, name: string, rand: () => number): string {
  const canName = name !== "" && greeting.named !== undefined;
  const useNamed =
    canName && (greeting.unnamed === undefined || rand() < 0.5);
  if (useNamed) return greeting.named!.replace("{name}", name);
  return greeting.unnamed ?? greeting.named!.replace("{name}", name);
}

// greetingForNow picks a time/day-appropriate greeting at random. `now` and `rand`
// are injectable so callers (and tests) can pin the moment and the choice.
export function greetingForNow(fullName: string, now = new Date(), rand = Math.random): string {
  const name = firstName(fullName);
  const eligible = eligibleGreetings(name, now);
  const pick = eligible[Math.floor(rand() * eligible.length)] ?? eligible[0];
  return renderGreeting(pick, name, rand);
}

// possibleGreetings enumerates every string greetingForNow could return at `now`
// for the given name — used by tests to assert membership without duplicating the
// pool.
export function possibleGreetings(fullName: string, now = new Date()): string[] {
  const name = firstName(fullName);
  const out = new Set<string>();
  for (const g of eligibleGreetings(name, now)) {
    if (name !== "" && g.named !== undefined) out.add(g.named.replace("{name}", name));
    if (g.unnamed !== undefined) out.add(g.unnamed);
  }
  return [...out];
}

export function isNearBottom(element: HTMLElement): boolean {
  return element.scrollHeight - element.scrollTop - element.clientHeight <= 48;
}

export function previousUserContent(messages: Message[], beforeIndex: number): string | null {
  for (let index = beforeIndex - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (message.role === "user") return message.content;
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
    placeholderID !== null ? messages.findIndex((message) => message.id === placeholderID) : -1;
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
