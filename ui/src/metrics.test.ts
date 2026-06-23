import { expect, test } from "vitest";
import { formatDuration, buildMetricsString, hasRenderableMetrics } from "./metrics";
import type { Message } from "./api";

function assistant(extra: Partial<Message>): Message {
  return { id: "m1", threadId: "t1", role: "assistant", content: "hi", createdAt: "2026-05-31T14:32:00Z", ...extra };
}

test("formatDuration shows seconds up to 120s, then m s above", () => {
  expect(formatDuration(250)).toBe("250ms");
  expect(formatDuration(5200)).toBe("5.2s");
  expect(formatDuration(90000)).toBe("90.0s");
  expect(formatDuration(120000)).toBe("120.0s");
  expect(formatDuration(121000)).toBe("2m 1s");
  expect(formatDuration(3_661_000)).toBe("1h 1m 1s");
});

test("hasRenderableMetrics requires duration and any token usage", () => {
  expect(hasRenderableMetrics(assistant({ durationMs: 2000, completionTokens: 100 }))).toBe(true);
  expect(hasRenderableMetrics(assistant({ durationMs: 2000, promptTokens: 900, totalTokens: 900 }))).toBe(true);
  expect(hasRenderableMetrics(assistant({ completionTokens: 100 }))).toBe(false);
  expect(hasRenderableMetrics(assistant({ durationMs: 2000 }))).toBe(false);
  expect(hasRenderableMetrics(assistant({ durationMs: 0, completionTokens: 100 }))).toBe(false);
  expect(hasRenderableMetrics(assistant({ durationMs: 2000, promptTokens: 0, completionTokens: 0, totalTokens: 0 }))).toBe(false);
});

test("buildMetricsString assembles model, duration, token counts and context %", () => {
  const line = buildMetricsString(
    assistant({ model: "mimo", durationMs: 5000, promptTokens: 49498, completionTokens: 1502, totalTokens: 51000, cachedTokens: 38208, reasoningTokens: 205 }),
  );
  // 51000 / 1048576 = 4.86% -> "4.9%"
  expect(line).toBe("mimo · 5.0s · ↑ 49 498 (38 208/c) · ↓ 1 502 (205/r) · 4.9%");
});

test("buildMetricsString appends the reasoning effort level to the model", () => {
  const line = buildMetricsString(
    assistant({ model: "mimo-v2.5-pro", reasoningEffort: "high", durationMs: 5000, promptTokens: 100000, completionTokens: 4858, totalTokens: 104858 }),
  );
  // 104858 / 1048576 = 10.0% -> "10.0%"
  expect(line).toBe("mimo-v2.5-pro (high) · 5.0s · ↑ 100 000 · ↓ 4 858 · 10.0%");
});

test("buildMetricsString renders completion-only token burn", () => {
  const line = buildMetricsString(assistant({ durationMs: 2000, completionTokens: 100 }));
  expect(line).toBe("2.0s · ↓ 100");
});

test("buildMetricsString shows prompt-only token burn", () => {
  const line = buildMetricsString(assistant({ durationMs: 1200, promptTokens: 52429, totalTokens: 52429 }));
  expect(line).toBe("1.2s · ↑ 52 429 · 5.0%");
});

test("buildMetricsString hides zero-valued token fields", () => {
  const line = buildMetricsString(assistant({ durationMs: 1200, promptTokens: 0, completionTokens: 100, totalTokens: 0 }));
  expect(line).toBe("1.2s · ↓ 100");
});

test("buildMetricsString reports context usage above 100% without clamping", () => {
  const line = buildMetricsString(assistant({ durationMs: 1000, completionTokens: 100, totalTokens: 2_000_000 }));
  // 2 000 000 / 1 048 576 = 190.7% -> overflow is shown honestly, not capped at 100%
  expect(line).toContain("190.7%");
});

test("buildMetricsString returns null without renderable metrics", () => {
  expect(buildMetricsString(assistant({ completionTokens: 100 }))).toBeNull();
});
