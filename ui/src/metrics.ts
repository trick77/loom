import type { Message } from "./api";

/**
 * Turn a snake_case prompt-classifier category into a display label for the pill
 * (e.g. "knowledge_discovery" -> "Knowledge Discovery"). Title case, with the
 * URL acronym kept upper-case ("url_lookup" -> "URL Lookup").
 */
export function humanizeCategory(category: string): string {
  const spaced = category.replace(/_/g, " ").trim();
  if (spaced === "") return "";
  const titled = spaced.replace(/\b\w/g, (c) => c.toUpperCase());
  return titled.replace(/\bUrl\b/, "URL");
}

/** Narrow no-break space (U+202F) — used as the thousands separator and after the arrows. */
const THIN_SPACE = " ";

/** Group integer thousands with a narrow no-break space (e.g. 1234 -> "1 234"). */
function groupThousands(value: number): string {
  return Math.round(value)
    .toString()
    .replace(/\B(?=(\d{3})+(?!\d))/g, THIN_SPACE);
}

function hasPositiveValue(value: number | undefined): value is number {
  return value !== undefined && value > 0;
}

/**
 * Segment separator: a middle dot with a widened gap on each side. The inner
 * U+00A0 no-break spaces supply the extra width (consecutive normal spaces
 * collapse to one in HTML); the outer normal spaces keep the line breakable at
 * separators.
 */
const DOT_SEPARATOR = " \u00A0\u00B7\u00A0 ";

/**
 * MiMo-V2.5-Pro's context window in tokens. Hardcoded here like the model name on
 * the backend (both are fixed) and used to show how full the context window is.
 */
const CONTEXT_WINDOW_TOKENS = 1_048_576;

/**
 * Format the context-window occupancy as a percentage (e.g. "5 %"), rounded to a
 * whole number with a narrow no-break space before the percent sign. contextTokens
 * is the final answer call's model-reported total_tokens — the true size of that
 * single generation's context — so this is bounded by the window by construction.
 */
function contextUsagePercent(contextTokens: number): string {
  return `${Math.round((contextTokens / CONTEXT_WINDOW_TOKENS) * 100)}${THIN_SPACE}%`;
}

/**
 * Format a nano-USD figure as dollars, to the cent ("$0.03", "$1.24"). A cent
 * is the smallest amount shown: a priced turn that cost less still reads
 * "$0.01", never "$0.00", so a paid call is never displayed as free. The
 * amount is a list-rate equivalent, not an invoice.
 */
export function formatCostNanoUsd(nanoUsd: number): string {
  // Round in integer cents: toFixed on the float would turn 0.045 into "0.04".
  const cents = Math.round(nanoUsd / 10_000_000);
  if (nanoUsd > 0 && cents < 1) return "$0.01";
  return `$${(cents / 100).toFixed(2)}`;
}

/**
 * The thread's cost up to and including the message at `index`: the sum of
 * every priced message before it in transcript order. Unpriced messages add
 * nothing, so the figure is a floor when some turns had no rate.
 */
export function threadCostThrough(messages: Message[], index: number): number {
  let sum = 0;
  for (let i = 0; i <= index && i < messages.length; i++) {
    const cost = messages[i].costNanoUsd;
    if (hasPositiveValue(cost)) sum += cost;
  }
  return sum;
}

function cachedSuffix(message: Message): string {
  return hasPositiveValue(message.cachedTokens)
    ? ` (${groupThousands(message.cachedTokens)}/c)`
    : "";
}

function reasoningSuffix(message: Message): string {
  return hasPositiveValue(message.reasoningTokens)
    ? ` (${groupThousands(message.reasoningTokens)}/r)`
    : "";
}

/** Format a duration in milliseconds: ms / s / m s / h m s. */
export function formatDuration(ms: number): string {
  if (ms < 0) return "";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const seconds = ms / 1000;
  if (seconds <= 120) return `${Math.round(seconds)}s`;
  if (seconds < 3600) {
    const m = Math.floor(seconds / 60);
    const s = Math.floor(seconds % 60);
    return `${m}m ${s}s`;
  }
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  return `${h}h ${m}m ${s}s`;
}

/** True when there is enough data to show a meaningful metrics line. */
export function hasRenderableMetrics(message: Message): boolean {
  return Boolean(
    message.durationMs &&
    message.durationMs > 0 &&
    (hasPositiveValue(message.promptTokens) ||
      hasPositiveValue(message.completionTokens) ||
      hasPositiveValue(message.totalTokens)),
  );
}

/**
 * Build the metrics line (effort · duration · ↑in (cached/c) · ↓out (reasoning/r) · context% ·
 * Σ $thread), or null when there is nothing renderable. The leading effort segment is
 * historical: loom sends no reasoning level any more, so new messages store none and the
 * line opens on the duration. It still renders for messages persisted before that change,
 * which is why the segment stays. The cost segment is the thread's running total
 * (`threadCostNanoUsd`, from threadCostThrough), never the turn's own figure: what the
 * reader wants next to the context gauge is what the conversation has cost so far.
 */
export function buildMetricsString(
  message: Message,
  threadCostNanoUsd?: number,
): string | null {
  if (!hasRenderableMetrics(message)) return null;
  const durationMs = message.durationMs as number;

  const segments: string[] = [];
  if (message.reasoningEffort) {
    segments.push(message.reasoningEffort);
  }
  segments.push(formatDuration(durationMs));
  if (
    hasPositiveValue(message.promptTokens) &&
    hasPositiveValue(message.completionTokens)
  ) {
    const up = `↑${THIN_SPACE}${groupThousands(message.promptTokens)}${cachedSuffix(message)}`;
    const down = `↓${THIN_SPACE}${groupThousands(message.completionTokens)}${reasoningSuffix(message)}`;
    segments.push(`${up}${DOT_SEPARATOR}${down}`);
  } else if (hasPositiveValue(message.promptTokens)) {
    segments.push(
      `↑${THIN_SPACE}${groupThousands(message.promptTokens)}${cachedSuffix(message)}`,
    );
  } else if (hasPositiveValue(message.completionTokens)) {
    segments.push(
      `↓${THIN_SPACE}${groupThousands(message.completionTokens)}${reasoningSuffix(message)}`,
    );
  }
  if (hasPositiveValue(message.contextTokens)) {
    segments.push(contextUsagePercent(message.contextTokens));
  }
  if (hasPositiveValue(threadCostNanoUsd)) {
    segments.push(`Σ${THIN_SPACE}${formatCostNanoUsd(threadCostNanoUsd)}`);
  }
  return segments.join(DOT_SEPARATOR);
}

/**
 * Format a message's own time as 24-hour HH:MM in the viewer's locale/timezone
 * (e.g. "07:30"). Empty string when createdAt is not a parseable timestamp.
 */
export function formatMessageTime(createdAt: string): string {
  const d = new Date(createdAt);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}
