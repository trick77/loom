import type { Schedule } from "../api";

// makeSchedule is a weekly schedule whose last run found two new items.
export function makeSchedule(overrides: Partial<Schedule> = {}): Schedule {
  return {
    id: "s1",
    name: "Cinema scout",
    prompt: "Find new films",
    interval: "once a week",
    cron: "37 14 * * 4",
    template: "~ 7~22 * * ~",
    timezone: "Europe/Zurich",
    enabled: true,
    nextRunAt: "2026-10-29T13:37:00Z",
    running: false,
    disabledReason: "",
    disabledAt: null,
    createdAt: "2026-10-01T00:00:00Z",
    updatedAt: "2026-10-01T00:00:00Z",
    summary: "Weekly on Thursday at 14:37",
    nextRuns: ["2026-10-29T13:37:00Z"],
    lastRun: {
      id: "r1",
      scheduleId: "s1",
      threadId: "th1",
      messageId: "m1",
      startedAt: "2026-10-22T12:37:00Z",
      finishedAt: "2026-10-22T12:39:00Z",
      status: "new",
      rounds: 3,
      tokens: 41000,
      durationMs: 108000,
      error: "",
    },
    ...overrides,
  };
}
