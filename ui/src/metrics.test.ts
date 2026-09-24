import { expect, test } from "vitest";
import {
  formatDuration,
  buildMetricsString,
  threadCostThrough,
  formatCostNanoUsd,
  formatMessageTime,
  hasRenderableMetrics,
  humanizeCategory,
} from "./metrics";
import type { Message } from "./api";

function assistant(extra: Partial<Message>): Message {
  return {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: "hi",
    createdAt: "2026-05-31T14:32:00Z",
    ...extra,
  };
}

test("formatDuration shows whole seconds up to 120s, then m s above", () => {
  expect(formatDuration(250)).toBe("250ms");
  expect(formatDuration(5200)).toBe("5s");
  expect(formatDuration(90000)).toBe("90s");
  expect(formatDuration(120000)).toBe("120s");
  expect(formatDuration(121000)).toBe("2m 1s");
  expect(formatDuration(3_661_000)).toBe("1h 1m 1s");
});

test("hasRenderableMetrics requires duration and any token usage", () => {
  expect(
    hasRenderableMetrics(
      assistant({ durationMs: 2000, completionTokens: 100 }),
    ),
  ).toBe(true);
  expect(
    hasRenderableMetrics(
      assistant({ durationMs: 2000, promptTokens: 900, totalTokens: 900 }),
    ),
  ).toBe(true);
  expect(hasRenderableMetrics(assistant({ completionTokens: 100 }))).toBe(
    false,
  );
  expect(hasRenderableMetrics(assistant({ durationMs: 2000 }))).toBe(false);
  expect(
    hasRenderableMetrics(assistant({ durationMs: 0, completionTokens: 100 })),
  ).toBe(false);
  expect(
    hasRenderableMetrics(
      assistant({
        durationMs: 2000,
        promptTokens: 0,
        completionTokens: 0,
        totalTokens: 0,
      }),
    ),
  ).toBe(false);
});

test("buildMetricsString omits the lead segment when the message stored no reasoning effort", () => {
  const line = buildMetricsString(
    assistant({
      model: "mimo",
      durationMs: 5000,
      promptTokens: 49498,
      completionTokens: 1502,
      totalTokens: 249000,
      contextTokens: 51000,
      cachedTokens: 38208,
      reasoningTokens: 205,
    }),
  );
  // The model name is never shown; with no reasoningEffort the line starts at duration.
  // The % comes from contextTokens (final call's model-reported total), NOT the
  // accumulated totalTokens (249000): 51000 / 1048576 = 4.86% -> "5 %"
  expect(line).toBe("5s  ·  ↑ 49 498 (38 208/c)  ·  ↓ 1 502 (205/r)  ·  5 %");
});

test("formatCostNanoUsd rounds to the cent and never shows a priced amount as free", () => {
  expect(formatCostNanoUsd(3_141_593)).toBe("$0.01");
  expect(formatCostNanoUsd(12_500_000)).toBe("$0.01");
  expect(formatCostNanoUsd(34_000_000)).toBe("$0.03");
  expect(formatCostNanoUsd(1_237_000_000)).toBe("$1.24");
  expect(formatCostNanoUsd(0)).toBe("$0.00");
});

test("buildMetricsString ends with the thread's running cost, and omits it when nothing was priced", () => {
  const priced = buildMetricsString(
    assistant({
      durationMs: 5000,
      promptTokens: 1000,
      completionTokens: 200,
      contextTokens: 51000,
      costNanoUsd: 3_141_593,
    }),
    3_141_593,
  );
  expect(priced).toBe("5s  ·  ↑ 1 000  ·  ↓ 200  ·  5 %  ·  Σ $0.01");
  const unpriced = buildMetricsString(
    assistant({ durationMs: 5000, promptTokens: 1000, completionTokens: 200 }),
  );
  expect(unpriced).toBe("5s  ·  ↑ 1 000  ·  ↓ 200");
});

test("buildMetricsString leads with a stored reasoning effort, without the model or parentheses", () => {
  const line = buildMetricsString(
    assistant({
      model: "mimo-v2.5-pro",
      reasoningEffort: "high",
      durationMs: 5000,
      promptTokens: 100000,
      completionTokens: 4858,
      totalTokens: 104858,
      contextTokens: 104858,
    }),
  );
  // 104858 / 1048576 = 10.0% -> "10 %"
  expect(line).toBe("high  ·  5s  ·  ↑ 100 000  ·  ↓ 4 858  ·  10 %");
});

test("humanizeCategory title-cases snake_case and upper-cases the URL acronym", () => {
  expect(humanizeCategory("url_lookup")).toBe("URL Lookup");
  expect(humanizeCategory("knowledge_discovery")).toBe("Knowledge Discovery");
  expect(humanizeCategory("")).toBe("");
});

test("buildMetricsString renders completion-only token burn", () => {
  const line = buildMetricsString(
    assistant({ durationMs: 2000, completionTokens: 100 }),
  );
  expect(line).toBe("2s  ·  ↓ 100");
});

test("buildMetricsString shows prompt-only token burn", () => {
  const line = buildMetricsString(
    assistant({
      durationMs: 1200,
      promptTokens: 52429,
      totalTokens: 52429,
      contextTokens: 52429,
    }),
  );
  expect(line).toBe("1s  ·  ↑ 52 429  ·  5 %");
});

test("buildMetricsString hides zero-valued token fields", () => {
  const line = buildMetricsString(
    assistant({
      durationMs: 1200,
      promptTokens: 0,
      completionTokens: 100,
      totalTokens: 0,
    }),
  );
  expect(line).toBe("1s  ·  ↓ 100");
});

test("buildMetricsString omits the context % for messages without contextTokens", () => {
  // Messages persisted before contextTokens existed carry only the accumulated
  // totalTokens. Show the line without a (wrong) percentage rather than dividing
  // the inflated accumulated total by the window.
  const line = buildMetricsString(
    assistant({
      durationMs: 5000,
      promptTokens: 49498,
      completionTokens: 1502,
      totalTokens: 249000,
    }),
  );
  expect(line).toBe("5s  ·  ↑ 49 498  ·  ↓ 1 502");
});

test("buildMetricsString formats context usage above 100% without clamping", () => {
  // A real single call cannot exceed the window, so this only documents the
  // formatting path: contextTokens drives the %, and it is never capped.
  const line = buildMetricsString(
    assistant({
      durationMs: 1000,
      completionTokens: 100,
      contextTokens: 2_000_000,
    }),
  );
  // 2 000 000 / 1 048 576 = 190.7% -> "191 %"
  expect(line).toContain("191 %");
});

test("buildMetricsString returns null without renderable metrics", () => {
  expect(buildMetricsString(assistant({ completionTokens: 100 }))).toBeNull();
});

test("formatMessageTime renders a 24-hour HH:MM clock, and empty for an unparseable timestamp", () => {
  // TZ-independent: assert the shape, not a hardcoded local time.
  expect(formatMessageTime("2026-05-31T14:32:00Z")).toMatch(/^\d{2}:\d{2}$/);
  expect(formatMessageTime("not-a-date")).toBe("");
});

test("buildMetricsString shows the thread's running total, not the turn's own cost", () => {
  const turn = assistant({
    durationMs: 5000,
    promptTokens: 1000,
    completionTokens: 200,
    costNanoUsd: 12_000_000,
  });
  expect(buildMetricsString(turn)).toBe("5s  ·  ↑ 1 000  ·  ↓ 200");
  expect(buildMetricsString(turn, 45_000_000)).toBe(
    "5s  ·  ↑ 1 000  ·  ↓ 200  ·  Σ $0.05",
  );
});

test("threadCostThrough sums the priced messages up to and including the index", () => {
  const messages = [
    assistant({ costNanoUsd: 10_000_000 }),
    assistant({}),
    assistant({ costNanoUsd: 25_000_000 }),
    assistant({ costNanoUsd: 5_000_000 }),
  ];
  expect(threadCostThrough(messages, 0)).toBe(10_000_000);
  expect(threadCostThrough(messages, 1)).toBe(10_000_000);
  expect(threadCostThrough(messages, 2)).toBe(35_000_000);
  expect(threadCostThrough(messages, 99)).toBe(40_000_000);
});
