import type { Message } from "./api";

/** Narrow no-break space (U+202F) — used as the thousands separator and after the arrows. */
const THIN_SPACE = " ";

/** Group integer thousands with a narrow no-break space (e.g. 1234 -> "1 234"). */
function groupThousands(value: number): string {
  return Math.round(value).toString().replace(/\B(?=(\d{3})+(?!\d))/g, THIN_SPACE);
}

function hasPositiveValue(value: number | undefined): value is number {
  return value !== undefined && value > 0;
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
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
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
 * Build the metrics line (model (effort) · duration · ↑in (cached/c) · ↓out (reasoning/r) · ∑total),
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
    segments.push(`${up} · ${down}`);
  } else if (hasPositiveValue(message.promptTokens)) {
    segments.push(`↑${THIN_SPACE}${groupThousands(message.promptTokens)}${cachedSuffix(message)}`);
  } else if (hasPositiveValue(message.completionTokens)) {
    segments.push(`↓${THIN_SPACE}${groupThousands(message.completionTokens)}${reasoningSuffix(message)}`);
  }
  if (hasPositiveValue(message.totalTokens)) {
    segments.push(`∑${THIN_SPACE}${groupThousands(message.totalTokens)}`);
  }
  return segments.join(" · ");
}
