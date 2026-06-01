import type { Message } from "./api";

/** Group integer thousands with a thin spacing (e.g. 1234 -> "1 234"). */
function groupThousands(value: number): string {
  return Math.round(value).toString().replace(/\B(?=(\d{3})+(?!\d))/g, " ");
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

/** Format tokens-per-second: 2 decimals below 1000, grouped above. */
export function formatTps(tps: number): string {
  return tps < 1000 ? tps.toFixed(2) : groupThousands(tps);
}

/** True when there is enough data to show a meaningful tok/s line. */
export function hasRenderableMetrics(message: Message): boolean {
  return Boolean(message.durationMs && message.durationMs > 0 && message.completionTokens);
}

/**
 * Build the metrics line (model · duration (tok/s) · tokens · cached · reasoning),
 * or null when there is nothing renderable. Timestamp is appended by the caller.
 */
export function buildMetricsString(message: Message): string | null {
  if (!hasRenderableMetrics(message)) return null;
  const durationMs = message.durationMs as number;
  const completionTokens = message.completionTokens as number;
  const outputTps = completionTokens / (durationMs / 1000);

  const segments: string[] = [];
  if (message.model) segments.push(message.model);
  segments.push(`${formatDuration(durationMs)} (${formatTps(outputTps)} tok/s)`);
  if (
    message.promptTokens !== undefined &&
    message.completionTokens !== undefined &&
    message.totalTokens !== undefined
  ) {
    segments.push(
      `${groupThousands(message.promptTokens)} → ${groupThousands(message.completionTokens)} (${groupThousands(message.totalTokens)} tok)`,
    );
  }
  if (message.cachedTokens && message.cachedTokens > 0) segments.push(`cached ${groupThousands(message.cachedTokens)}`);
  if (message.reasoningTokens && message.reasoningTokens > 0) segments.push(`reasoning ${groupThousands(message.reasoningTokens)}`);
  return segments.join(" · ");
}
