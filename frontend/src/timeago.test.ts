import { describe, expect, it } from "vitest";

import { formatTimeAgo } from "./timeago";

const now = new Date("2026-06-04T18:00:00Z");
const ago = (ms: number) => new Date(now.getTime() - ms).toISOString();

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

describe("formatTimeAgo", () => {
  it("returns 'just now' for sub-minute and future timestamps", () => {
    expect(formatTimeAgo(ago(5_000), now)).toBe("just now");
    expect(formatTimeAgo(new Date(now.getTime() + HOUR).toISOString(), now)).toBe("just now");
  });

  it("formats minutes with singular/plural", () => {
    expect(formatTimeAgo(ago(MIN), now)).toBe("1 minute ago");
    expect(formatTimeAgo(ago(3 * MIN), now)).toBe("3 minutes ago");
  });

  it("formats hours", () => {
    expect(formatTimeAgo(ago(3 * HOUR), now)).toBe("3 hours ago");
    expect(formatTimeAgo(ago(20 * HOUR), now)).toBe("20 hours ago");
  });

  it("special-cases yesterday (24-48h), not '1 day ago'", () => {
    expect(formatTimeAgo(ago(25 * HOUR), now)).toBe("yesterday");
    expect(formatTimeAgo(ago(47 * HOUR), now)).toBe("yesterday");
  });

  it("formats whole days from 48h onward", () => {
    expect(formatTimeAgo(ago(2 * DAY), now)).toBe("2 days ago");
    expect(formatTimeAgo(ago(6 * DAY), now)).toBe("6 days ago");
  });

  it("returns empty string for invalid input", () => {
    expect(formatTimeAgo("not-a-date", now)).toBe("");
  });
});
