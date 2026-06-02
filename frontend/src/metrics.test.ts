import { expect, test } from "vitest";
import { formatDuration, buildMetricsString, hasRenderableMetrics } from "./metrics";
import type { Message } from "./api";

function assistant(extra: Partial<Message>): Message {
  return { id: "m1", threadId: "t1", role: "assistant", content: "hi", createdAt: "2026-05-31T14:32:00Z", ...extra };
}

test("formatDuration scales by magnitude", () => {
  expect(formatDuration(250)).toBe("250ms");
  expect(formatDuration(5200)).toBe("5.2s");
  expect(formatDuration(90000)).toBe("1m 30s");
  expect(formatDuration(3_661_000)).toBe("1h 1m 1s");
});

test("hasRenderableMetrics requires duration and completion tokens", () => {
  expect(hasRenderableMetrics(assistant({ durationMs: 2000, completionTokens: 100 }))).toBe(true);
  expect(hasRenderableMetrics(assistant({ completionTokens: 100 }))).toBe(false);
  expect(hasRenderableMetrics(assistant({ durationMs: 2000 }))).toBe(false);
  expect(hasRenderableMetrics(assistant({ durationMs: 0, completionTokens: 100 }))).toBe(false);
});

test("buildMetricsString assembles model, duration and token counts", () => {
  const line = buildMetricsString(
    assistant({ model: "mimo", durationMs: 5000, promptTokens: 1234, completionTokens: 500, totalTokens: 1734, cachedTokens: 128, reasoningTokens: 64 }),
  );
  expect(line).toBe("mimo · 5.0s · ↑ 1 234 (128/c) ↓ 500 (64/r)");
});

test("buildMetricsString appends the reasoning effort level to the model", () => {
  const line = buildMetricsString(
    assistant({ model: "mimo-v2.5-pro", reasoningEffort: "high", durationMs: 5000, promptTokens: 10, completionTokens: 500, totalTokens: 510 }),
  );
  expect(line).toBe("mimo-v2.5-pro (high) · 5.0s · ↑ 10 ↓ 500");
});

test("buildMetricsString omits absent segments", () => {
  const line = buildMetricsString(assistant({ durationMs: 2000, completionTokens: 100 }));
  expect(line).toBe("2.0s");
});

test("buildMetricsString returns null without renderable metrics", () => {
  expect(buildMetricsString(assistant({ completionTokens: 100 }))).toBeNull();
});
