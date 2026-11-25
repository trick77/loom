import { expect, test } from "vitest";

import type { Artifact } from "../api";
import {
  type ArtifactGroup,
  groupArtifacts,
  newestOlderYear,
  periodBounds,
  periodKey,
  recentWindowStart,
  summaryMonths,
  summaryYears,
} from "./artifactGroups";

// Local midday, so no zone puts "now" on another calendar day.
const now = new Date(2026, 10, 25, 12, 0, 0);

function at(date: Date, id: string, mimeType = "image/png"): Artifact {
  return {
    id,
    displayFilename: `${id}.bin`,
    mimeType,
    sizeBytes: 1,
    modifiedAt: date.toISOString(),
    downloadUrl: `/api/artifacts/${id}/download`,
  };
}

// runsOf is a group's runs as [kind, ids] pairs.
const runsOf = (group: ArtifactGroup) =>
  group.runs.map((run) => [run.kind, run.items.map((a) => a.id)]);

test("the recent window starts at local midnight 30 days ago", () => {
  expect(recentWindowStart(now)).toEqual(new Date(2026, 9, 26));
});

test("recent items fall into today, this week and this month, newest first", () => {
  const items = [
    at(new Date(2026, 10, 25, 9), "today-img"),
    at(new Date(2026, 10, 25, 0, 0, 1), "today-file", "text/plain"),
    at(new Date(2026, 10, 24, 23, 59), "yesterday"),
    // Like the 30-day window, the week starts at local midnight 7 days ago.
    at(new Date(2026, 10, 18, 0, 0), "week-edge"),
    at(new Date(2026, 10, 17, 23, 59), "month-start", "application/pdf"),
    at(new Date(2026, 9, 26), "month-end"),
    // Older than the window: the server owns the window, so it stays in the
    // last group rather than vanishing.
    at(new Date(2026, 5, 10), "stray"),
  ];
  const groups = groupArtifacts(items, now, "recent");
  expect(groups.map((group) => group.kind)).toEqual(["today", "week", "month"]);
  // The window comes in one request: each group shows its tiles, then its
  // rows, whatever order they came in.
  expect(runsOf(groups[0])).toEqual([
    ["tiles", ["today-img"]],
    ["rows", ["today-file"]],
  ]);
  expect(runsOf(groups[1])).toEqual([["tiles", ["yesterday", "week-edge"]]]);
  expect(runsOf(groups[2])).toEqual([
    ["tiles", ["month-end", "stray"]],
    ["rows", ["month-start"]],
  ]);
  expect(groups.map((group) => group.size)).toEqual([2, 2, 3]);
});

test("a recent window in pages keeps its groups, in arrival order", () => {
  const items = [
    at(new Date(2026, 10, 25, 9), "today-file", "text/plain"),
    at(new Date(2026, 10, 25, 8), "today-img"),
    at(new Date(2026, 10, 20), "week"),
    at(new Date(2026, 10, 1), "month", "application/pdf"),
  ];
  const groups = groupArtifacts(items, now, "recentPages");
  expect(groups.map((group) => group.kind)).toEqual(["today", "week", "month"]);
  // The file came first, so it stays above the image.
  expect(runsOf(groups[0])).toEqual([
    ["rows", ["today-file"]],
    ["tiles", ["today-img"]],
  ]);
  expect(groups.map((group) => group.size)).toEqual([2, 1, 1]);
});

test("empty recent groups are left out; an undated item counts as oldest", () => {
  const undated: Artifact = { ...at(now, "undated"), modifiedAt: undefined };
  const groups = groupArtifacts(
    [at(new Date(2026, 10, 25, 8), "a"), undated],
    now,
    "recent",
  );
  expect(groups.map((group) => group.kind)).toEqual(["today", "month"]);
  expect(runsOf(groups[1])).toEqual([["tiles", ["undated"]]]);
  expect(groupArtifacts([], now, "recent")).toEqual([]);
});

test("period views group by local calendar month in arrival order", () => {
  const items = [
    at(new Date(2025, 11, 31, 23, 30), "dec-img"),
    at(new Date(2025, 11, 1, 0, 5), "dec-file", "text/plain"),
    at(new Date(2025, 9, 15), "oct"),
    { ...at(now, "undated"), modifiedAt: "not a date" },
  ];
  const groups = groupArtifacts(items, now, "months");
  expect(groups.map((group) => [group.kind, group.year, group.month])).toEqual([
    ["calendarMonth", 2025, 12],
    ["calendarMonth", 2025, 10],
    ["all", undefined, undefined],
  ]);
  expect(runsOf(groups[0])).toEqual([
    ["tiles", ["dec-img"]],
    ["rows", ["dec-file"]],
  ]);
  expect(runsOf(groups[2])).toEqual([["tiles", ["undated"]]]);
});

// A period, a search, or a recent window too big for one request loads
// page-wise: a later page only appends, so an image of page 2 never lands
// above a file row already on screen.
test("page-wise views keep arrival order in runs of tiles and rows", () => {
  const page1 = [
    at(new Date(2025, 11, 20), "img-1"),
    at(new Date(2025, 11, 19), "file-1", "text/plain"),
  ];
  const page2 = [
    at(new Date(2025, 11, 18), "img-2"),
    at(new Date(2025, 11, 17), "img-3"),
    at(new Date(2025, 11, 16), "file-2", "application/pdf"),
    at(new Date(2025, 11, 15), "file-3", "text/plain"),
  ];
  for (const mode of ["months", "flat", "recentPages"] as const) {
    const before = groupArtifacts(page1, now, mode);
    const after = groupArtifacts([...page1, ...page2], now, mode);
    expect(after).toHaveLength(1);
    expect(runsOf(after[0])).toEqual([
      ["tiles", ["img-1"]],
      ["rows", ["file-1"]],
      ["tiles", ["img-2", "img-3"]],
      ["rows", ["file-2", "file-3"]],
    ]);
    // The runs already shown keep their keys, so React keeps their DOM.
    expect(after[0].runs.slice(0, 2).map((run) => run.key)).toEqual(
      before[0].runs.map((run) => run.key),
    );
    expect(after[0].size).toBe(6);
  }
});

test("search results are one ungrouped list in arrival order", () => {
  const groups = groupArtifacts(
    [
      at(new Date(2020, 0, 1), "old", "text/plain"),
      at(new Date(2026, 10, 25), "new"),
    ],
    now,
    "flat",
  );
  expect(groups).toHaveLength(1);
  expect(groups[0].kind).toBe("all");
  expect(runsOf(groups[0])).toEqual([
    ["rows", ["old"]],
    ["tiles", ["new"]],
  ]);
  expect(groupArtifacts([], now, "flat")).toEqual([]);
});

test("a year or month period spans local boundaries", () => {
  expect(periodBounds({ kind: "year", year: 2025 })).toEqual({
    since: new Date(2025, 0, 1),
    until: new Date(2026, 0, 1),
  });
  expect(periodBounds({ kind: "month", year: 2025, month: 12 })).toEqual({
    since: new Date(2025, 11, 1),
    until: new Date(2026, 0, 1),
  });
  expect(periodBounds({ kind: "recent" })).toBeNull();
  expect(periodKey({ kind: "recent" })).toBe("recent");
  expect(periodKey({ kind: "year", year: 2025 })).toBe("2025");
  expect(periodKey({ kind: "month", year: 2025, month: 3 })).toBe("2025-03");
});

test("summary months and years parse without shifting a month", () => {
  const months = [
    { month: "2026-11", count: 4 },
    { month: "2026-01", count: 2 },
    { month: "2024-12", count: 1 },
    { month: "garbage", count: 9 },
  ];
  expect(summaryMonths(months)).toEqual([
    { key: "2026-11", year: 2026, month: 11, count: 4 },
    { key: "2026-01", year: 2026, month: 1, count: 2 },
    { key: "2024-12", year: 2024, month: 12, count: 1 },
  ]);
  expect(summaryYears(months)).toEqual([2026, 2024]);
});

test("the newest year holding older items is found by counting down from the top", () => {
  const months = [
    { month: "2026-11", count: 5 },
    { month: "2026-10", count: 3 },
    { month: "2025-06", count: 2 },
  ];
  // 8 recent items fill November and October: the older ones live in 2025.
  expect(newestOlderYear({ olderCount: 2, months }, 2026)).toBe(2025);
  // October holds both recent and older items.
  expect(newestOlderYear({ olderCount: 3, months }, 2026)).toBe(2026);
  expect(newestOlderYear({ olderCount: 0, months }, 2026)).toBeNull();
  // Counts that do not add up fall back to the window's year.
  expect(newestOlderYear({ olderCount: 4, months: [] }, 2026)).toBe(2026);
});
