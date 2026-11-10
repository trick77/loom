import { expect, test } from "vitest";

import {
  greetingForNow,
  possibleGreetings,
  reconcileUserMessage,
  updateMessageAttachment,
  previousUserMessages,
  previousUserMessage,
} from "./threadUtils";
import type { GreetingVisit } from "./greetingVisit";
import type { MessageWithActivityTrace } from "./types";

function userMessage(
  overrides: Partial<MessageWithActivityTrace> & { id: string },
): MessageWithActivityTrace {
  return {
    threadId: "t1",
    role: "user",
    content: "Hello",
    createdAt: "2026-06-14T00:00:00Z",
    ...overrides,
  };
}

test("replaces the optimistic placeholder in place, keeping its clientKey", () => {
  const messages = [
    userMessage({ id: "m0", content: "earlier" }),
    userMessage({ id: "temp-1", clientKey: "temp-1", content: "hi" }),
  ];

  const result = reconcileUserMessage(
    messages,
    "temp-1",
    userMessage({ id: "m1", content: "hi" }),
  );

  expect(result.map((m) => m.id)).toEqual(["m0", "m1"]);
  // clientKey carried over from the placeholder => stable React key, no remount.
  expect(result[1].clientKey).toBe("temp-1");
});

test("replaces in place even when called after the reset that caused the bug", () => {
  // Regression: the original code read optimisticUserMessageID *inside* the deferred
  // updater, which React could run after it had been reset to null — losing the
  // placeholder and appending a second bubble. The fix captures the temp id into a
  // const first, so this call always receives the real placeholder id and dedups.
  const messages = [
    userMessage({ id: "temp-1", clientKey: "temp-1", content: "hi" }),
  ];

  const result = reconcileUserMessage(
    messages,
    "temp-1",
    userMessage({ id: "m1", content: "hi" }),
  );

  expect(result.map((m) => m.id)).toEqual(["m1"]);
  expect(result[0].clientKey).toBe("temp-1");
});

test("preserves an already-loaded copy on a duplicate/late event (no key flip or field overwrite)", () => {
  // A route refresh loaded the persisted message (richer fields, keyed off its id),
  // and the placeholder is already gone. A delayed/duplicate user_message must not
  // remount the bubble (clientKey flip) or revert its fields to the streamed payload.
  const loaded = userMessage({
    id: "m1",
    content: "hi",
    createdAt: "2026-06-14T00:00:01Z",
    attachments: [
      {
        id: "att-1",
        filename: "a.pdf",
        mimeType: "application/pdf",
        sizeBytes: 1,
        status: "ready",
      },
    ],
  });
  const messages = [userMessage({ id: "m0", content: "earlier" }), loaded];

  const result = reconcileUserMessage(
    messages,
    null,
    userMessage({ id: "m1", content: "hi" }),
  );

  expect(result).toEqual(messages);
  expect(result[1]).toBe(loaded); // same object reference => no remount, fields intact
});

test("appends once when no placeholder was inserted (id null, no temp bubble)", () => {
  // placeholderID is null only when the optimistic insert was skipped, so the list
  // holds no temp bubble to reconcile — the confirmed message is simply added once.
  const messages = [userMessage({ id: "m0", content: "earlier" })];

  const result = reconcileUserMessage(
    messages,
    null,
    userMessage({ id: "m1", content: "hi" }),
  );

  expect(result.map((m) => m.id)).toEqual(["m0", "m1"]);
});

test("is idempotent when a copy of the confirmed message is already present", () => {
  // A route refresh may have loaded the persisted message while the placeholder lingers.
  const messages = [
    userMessage({ id: "m1", content: "hi" }),
    userMessage({ id: "temp-1", clientKey: "temp-1", content: "hi" }),
  ];

  const result = reconcileUserMessage(
    messages,
    "temp-1",
    userMessage({ id: "m1", content: "hi" }),
  );

  expect(result.filter((m) => m.id === "m1")).toHaveLength(1);
  expect(result.map((m) => m.id)).toEqual(["m1"]);
});

test("appends when neither the placeholder nor a confirmed copy exist", () => {
  const messages = [userMessage({ id: "m0", content: "earlier" })];

  const result = reconcileUserMessage(
    messages,
    "temp-gone",
    userMessage({ id: "m1", content: "hi" }),
  );

  expect(result.map((m) => m.id)).toEqual(["m0", "m1"]);
  expect(result[1].clientKey).toBe("m1");
});

test("updates an attachment inside an already rendered user message", () => {
  const messages: MessageWithActivityTrace[] = [
    {
      id: "m1",
      threadId: "t1",
      role: "user",
      content: "Summarize this",
      createdAt: "2026-06-14T00:00:00Z",
      attachments: [
        {
          id: "att-1",
          filename: "briefing.pdf",
          mimeType: "application/pdf",
          sizeBytes: 1024,
          status: "uploading",
        },
      ],
    },
  ];

  const updated = updateMessageAttachment(messages, "att-1", {
    status: "ready",
    documentId: "doc-1",
  });

  expect(updated[0].attachments?.[0]).toMatchObject({
    id: "att-1",
    status: "ready",
    documentId: "doc-1",
  });
  expect(messages[0].attachments?.[0].status).toBe("uploading");
});

const zero = () => 0;
const firstToday: GreetingVisit = { firstToday: true, awayDays: null };
const laterToday: GreetingVisit = { firstToday: false, awayDays: 0 };
// Friday 9 January 2026, 09:00 local (past the New Year days).
const fridayMorning = new Date(2026, 0, 9, 9, 0, 0);

// every rand value from 0 up to (not including) 1 in `steps` steps, so a test sees
// every entry a weighted pick can land on.
function allPicks(
  name: string,
  now: Date,
  visit: GreetingVisit | undefined,
  steps = 400,
): Set<string> {
  const out = new Set<string>();
  for (let i = 0; i < steps; i += 1) {
    const rand = () => i / steps;
    out.add(greetingForNow(name, now, rand, visit));
  }
  return out;
}

test("the first start screen of the day greets by first name", () => {
  expect(greetingForNow("Jan Novak", fridayMorning, zero, firstToday)).toBe(
    "Good morning, Jan",
  );
  for (const g of allPicks("Jan Novak", fridayMorning, firstToday)) {
    expect(g).toContain("Jan");
  }
});

test("later the same day the greeting carries no name", () => {
  const picks = allPicks("Jan", fridayMorning, laterToday);
  expect(picks.size).toBeGreaterThan(5);
  for (const g of picks) {
    expect(g).not.toContain("Jan");
  }
});

test("a week away welcomes the user back by name", () => {
  const visit: GreetingVisit = { firstToday: true, awayDays: 7 };
  expect(allPicks("Jan", fridayMorning, visit)).toEqual(
    new Set(["Welcome back, Jan"]),
  );
  const sixDays: GreetingVisit = { firstToday: true, awayDays: 6 };
  expect(allPicks("Jan", fridayMorning, sixDays)).not.toContain(
    "Welcome back, Jan",
  );
});

test("a holiday beats the time-of-day salutation and the welcome back", () => {
  const christmasMorning = new Date(2026, 11, 25, 9, 0, 0);
  expect(allPicks("Jan", christmasMorning, firstToday)).toEqual(
    new Set(["Merry Christmas, Jan"]),
  );
  const backFromAWeekAway: GreetingVisit = { firstToday: true, awayDays: 8 };
  expect(allPicks("Jan", christmasMorning, backFromAWeekAway)).toEqual(
    new Set(["Merry Christmas, Jan"]),
  );
});

test("salutations never put the name on a question", () => {
  const night = new Date(2026, 0, 9, 23, 30, 0);
  for (const now of [fridayMorning, night]) {
    for (const g of allPicks("Jan", now, firstToday)) {
      expect(g).not.toMatch(/\?$/);
    }
  }
});

test("without a name the salutation drops it and no placeholder leaks", () => {
  expect(greetingForNow("", fridayMorning, zero, firstToday)).toBe(
    "Good morning",
  );
  const night = new Date(2026, 0, 2, 23, 30, 0);
  for (const visit of [firstToday, laterToday, undefined]) {
    for (const g of allPicks("", night, visit)) {
      expect(g).not.toMatch(/\{|,\s*$|,\s*\?/);
    }
  }
});

test("without storage the name shows up only sometimes", () => {
  const picks = allPicks("Jan", fridayMorning, undefined);
  const named = [...picks].filter((g) => g.includes("Jan"));
  expect(named.length).toBeGreaterThan(0);
  expect(named.length).toBeLessThan(picks.size);
});

test("time-of-day lines appear only in their band", () => {
  const night = new Date(2026, 0, 2, 23, 30, 0);
  const lunch = new Date(2026, 0, 6, 12, 0, 0);
  const evening = new Date(2026, 0, 6, 19, 0, 0);

  expect(possibleGreetings("Jan", fridayMorning)).toContain("Coffee first?");
  expect(possibleGreetings("Jan", fridayMorning)).not.toContain(
    "Hello, night owl",
  );
  expect(possibleGreetings("Jan", night)).toContain("Hello, night owl");
  expect(possibleGreetings("Jan", night)).not.toContain("Coffee first?");
  expect(possibleGreetings("Jan", lunch)).toContain("Lunch break?");
  expect(possibleGreetings("Jan", evening)).not.toContain("Lunch break?");
  // "How was your day?" is an evening question, never a morning one.
  expect(possibleGreetings("Jan", evening)).toContain("How was your day?");
  expect(possibleGreetings("Jan", fridayMorning)).not.toContain(
    "How was your day?",
  );
});

test("weekday and date lines appear only on their day", () => {
  const mondayMorning = new Date(2026, 0, 5, 9, 0, 0);
  const newYearsEve = new Date(2026, 11, 31, 15, 0, 0);
  // Guard against a wrong assumption about these calendar dates.
  expect(fridayMorning.getDay()).toBe(5);
  expect(mondayMorning.getDay()).toBe(1);

  expect(possibleGreetings("Jan", fridayMorning)).toContain("Finally Friday");
  expect(possibleGreetings("Jan", mondayMorning)).not.toContain(
    "Finally Friday",
  );
  expect(possibleGreetings("Jan", mondayMorning)).toContain(
    "Fresh week, fresh start",
  );
  expect(possibleGreetings("Jan", newYearsEve)).toContain(
    "Last day of the year",
  );
  expect(possibleGreetings("Jan", fridayMorning)).not.toContain(
    "Last day of the year",
  );
});

test("lines tied to a moment are picked more often than generic ones", () => {
  // Lunch on a Tuesday: lunchBreak is the only specific line, weighted 2.
  const lunch = new Date(2026, 0, 6, 12, 0, 0);
  const counts = new Map<string, number>();
  for (let i = 0; i < 1000; i += 1) {
    const g = greetingForNow("Jan", lunch, () => i / 1000, laterToday);
    counts.set(g, (counts.get(g) ?? 0) + 1);
  }
  expect(counts.get("Lunch break?")).toBeGreaterThan(
    counts.get("What's new?") ?? 0,
  );
});

test("previousUserMessages equals previousUserMessage at every index", () => {
  const messages = [
    {
      id: "u1",
      threadId: "t",
      role: "user" as const,
      content: "q1",
      createdAt: "",
    },
    {
      id: "a1",
      threadId: "t",
      role: "assistant" as const,
      content: "a1",
      createdAt: "",
    },
    {
      id: "u2",
      threadId: "t",
      role: "user" as const,
      content: "q2",
      createdAt: "",
    },
    {
      id: "a2",
      threadId: "t",
      role: "assistant" as const,
      content: "a2",
      createdAt: "",
    },
  ];
  const previous = previousUserMessages(messages);
  messages.forEach((_, index) => {
    expect(previous[index]).toBe(previousUserMessage(messages, index));
  });
});
