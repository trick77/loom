import type { Message } from "./api";

/**
 * Turn a snake_case prompt-classifier category into a display label for the pill
 * (e.g. "knowledge_discovery" -> "Knowledge discovery"). Sentence case.
 */
export function humanizeCategory(category: string): string {
  const spaced = category.replace(/_/g, " ").trim();
  if (spaced === "") return "";
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

/** Narrow no-break space (U+202F) — used as the thousands separator and after the arrows. */
const THIN_SPACE = " ";

/** Group integer thousands with a narrow no-break space (e.g. 1234 -> "1 234"). */
function groupThousands(value: number): string {
  return Math.round(value).toString().replace(/\B(?=(\d{3})+(?!\d))/g, THIN_SPACE);
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
 * Format the context-window occupancy as a percentage (e.g. "4.9%"). contextTokens
 * is the final answer call's model-reported total_tokens — the true size of that
 * single generation's context — so this is bounded by the window by construction.
 */
function contextUsagePercent(contextTokens: number): string {
  return `${((contextTokens / CONTEXT_WINDOW_TOKENS) * 100).toFixed(1)}%`;
}

function cachedSuffix(message: Message): string {
  return hasPositiveValue(message.cachedTokens) ? ` (${groupThousands(message.cachedTokens)}/c)` : "";
}

function reasoningSuffix(message: Message): string {
  return hasPositiveValue(message.reasoningTokens) ? ` (${groupThousands(message.reasoningTokens)}/r)` : "";
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
      (hasPositiveValue(message.promptTokens) || hasPositiveValue(message.completionTokens) || hasPositiveValue(message.totalTokens)),
  );
}

/**
 * Build the metrics line (model (effort) · duration · ↑in (cached/c) · ↓out (reasoning/r) · context%),
 * or null when there is nothing renderable.
 */
export function buildMetricsString(message: Message): string | null {
  if (!hasRenderableMetrics(message)) return null;
  const durationMs = message.durationMs as number;

  const segments: string[] = [];
  if (message.model) {
    segments.push(message.reasoningEffort ? `${message.model} (${message.reasoningEffort})` : message.model);
  }
  segments.push(formatDuration(durationMs));
  if (hasPositiveValue(message.promptTokens) && hasPositiveValue(message.completionTokens)) {
    const up = `↑${THIN_SPACE}${groupThousands(message.promptTokens)}${cachedSuffix(message)}`;
    const down = `↓${THIN_SPACE}${groupThousands(message.completionTokens)}${reasoningSuffix(message)}`;
    segments.push(`${up}${DOT_SEPARATOR}${down}`);
  } else if (hasPositiveValue(message.promptTokens)) {
    segments.push(`↑${THIN_SPACE}${groupThousands(message.promptTokens)}${cachedSuffix(message)}`);
  } else if (hasPositiveValue(message.completionTokens)) {
    segments.push(`↓${THIN_SPACE}${groupThousands(message.completionTokens)}${reasoningSuffix(message)}`);
  }
  if (hasPositiveValue(message.contextTokens)) {
    segments.push(contextUsagePercent(message.contextTokens));
  }
  return segments.join(DOT_SEPARATOR);
}
