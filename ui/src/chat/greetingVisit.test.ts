import { afterEach, expect, test, vi } from "vitest";

import { readGreetingVisit, rememberGreetingVisit } from "./greetingVisit";

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

const morning = new Date(2026, 10, 10, 7, 30, 0);

test("a browser that never saw loom counts as the first visit of the day", () => {
  expect(readGreetingVisit("u1", morning)).toEqual({
    firstToday: true,
    awayDays: null,
  });
});

test("a second start screen on the same day is not the first", () => {
  rememberGreetingVisit("u1", morning);
  expect(readGreetingVisit("u1", new Date(2026, 10, 10, 22, 0, 0))).toEqual({
    firstToday: false,
    awayDays: 0,
  });
});

test("the greeting day runs to 05:00, so midnight doesn't greet a night owl twice", () => {
  rememberGreetingVisit("u1", new Date(2026, 10, 10, 23, 40, 0));
  expect(readGreetingVisit("u1", new Date(2026, 10, 11, 0, 10, 0))).toEqual({
    firstToday: false,
    awayDays: 0,
  });
  expect(readGreetingVisit("u1", new Date(2026, 10, 11, 5, 0, 0))).toEqual({
    firstToday: true,
    awayDays: 1,
  });
});

test("counts greeting days away, not 24h periods", () => {
  rememberGreetingVisit("u1", new Date(2026, 10, 3, 23, 50, 0));
  expect(readGreetingVisit("u1", new Date(2026, 10, 10, 5, 10, 0))).toEqual({
    firstToday: true,
    awayDays: 7,
  });
});

test("each user on a shared browser keeps their own visit", () => {
  rememberGreetingVisit("u1", morning);
  expect(readGreetingVisit("u2", morning)).toEqual({
    firstToday: true,
    awayDays: null,
  });
});

test("a clock set back is a new day, never a negative absence", () => {
  rememberGreetingVisit("u1", new Date(2026, 10, 12, 9, 0, 0));
  expect(readGreetingVisit("u1", morning)).toEqual({
    firstToday: true,
    awayDays: null,
  });
});

test("garbage in storage reads as a first visit", () => {
  localStorage.setItem("loom.greeting.lastSeen.u1", "yesterday");
  expect(readGreetingVisit("u1", morning)).toEqual({
    firstToday: true,
    awayDays: null,
  });
});

test("blocked storage yields no visit and remembering does not throw", () => {
  vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new Error("blocked");
  });
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("blocked");
  });
  expect(readGreetingVisit("u1", morning)).toBeUndefined();
  expect(() => rememberGreetingVisit("u1", morning)).not.toThrow();
});
