import { expect, test } from "vitest";

import {
  describeCron,
  formatDuration,
  formatRunDate,
  formatTokens,
  slotText,
} from "./scheduleText";

const words = {
  daily: "Daily",
  weekdays: "Monday to Friday",
  weekly: (day: string) => `Weekly on ${day}`,
  days: (days: string) => `On ${days}`,
  monthly: (days: string) => `Monthly on the ${days}`,
  inMonths: (months: string) => `in ${months}`,
  at: (times: string) => `at ${times}`,
};

test("describeCron reads the common shapes", () => {
  expect(describeCron("37 14 * * 4", "en", words)).toBe(
    "Weekly on Thursday at 14:37",
  );
  expect(describeCron("52 7 * * 1-5", "en", words)).toBe(
    "Monday to Friday at 07:52",
  );
  expect(describeCron("5 19 * * *", "en", words)).toBe("Daily at 19:05");
  expect(describeCron("12 8,20 * * *", "en", words)).toBe(
    "Daily at 08:12 and 20:12",
  );
  expect(describeCron("14 10 4,19 * *", "en", words)).toBe(
    "Monthly on the 4 and 19 at 10:14",
  );
  expect(describeCron("0 9 * * 1,3", "en", words)).toBe(
    "On Monday and Wednesday at 09:00",
  );
  expect(describeCron("0 9 1 1,7 *", "en", words)).toBe(
    "Monthly on the 1 in Jan, Jul at 09:00",
  );
  expect(describeCron("0 9 * * 7", "en", words)).toBe(
    "Weekly on Sunday at 09:00",
  );
  expect(describeCron("37 14 * * 4", "de", words)).toBe(
    "Weekly on Donnerstag at 14:37",
  );
});

test("describeCron leaves other shapes to the server's summary", () => {
  expect(describeCron("0 9 1 * 1", "en", words)).toBeNull();
  expect(describeCron("*/30 9 * * *", "en", words)).toBeNull();
  expect(describeCron("0 1,2,3,4 * * *", "en", words)).toBeNull();
  expect(describeCron("a b c d e", "en", words)).toBeNull();
  expect(describeCron("0 9 * *", "en", words)).toBeNull();
  expect(describeCron("0 9 */0 * *", "en", words)).toBeNull();
  expect(slotText("0 9 1 * 1", "On cron 0 9 1 * 1", "en", (k) => k)).toBe(
    "On cron 0 9 1 * 1",
  );
  expect(
    slotText("0 9 * * *", "x", "en", (k, o) => `${k}${o?.times ?? ""}`),
  ).toBe("schedules.slot.daily schedules.slot.at09:00");
});

test("formatRunDate shows the time on the schedule's clock", () => {
  expect(formatRunDate("2026-10-29T13:37:00Z", "en-GB", "Europe/Zurich")).toBe(
    "Thu 29 Oct, 14:37",
  );
  expect(formatRunDate("not a date", "en")).toBe("not a date");
  expect(
    formatRunDate("2026-10-29T13:37:00Z", "en-GB", "Mars/Olympus"),
  ).toMatch(/29 Oct/);
});

test("formatTokens and formatDuration", () => {
  expect(formatTokens(950)).toBe("950");
  expect(formatTokens(41_000)).toBe("41k");
  expect(formatTokens(300_000)).toBe("300k");
  expect(formatTokens(1_200_000)).toBe("1.2M");
  expect(formatTokens(2_000_000)).toBe("2M");
  expect(formatDuration(108_000)).toBe("1m 48s");
  expect(formatDuration(9_000)).toBe("0m 09s");
  expect(formatDuration(-5)).toBe("0m 00s");
});
