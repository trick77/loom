import type { Message } from "./api";
import { buildMetricsString } from "./metrics";

/**
 * Renders the stats line for an assistant message, right-aligned via `ml-auto`
 * within the actions row. Hover reveal via the plain-CSS `.spark-action-reveal`
 * (see index.css) — not Tailwind group-hover, which Safari mishandles under v4's
 * `@media (hover: hover)` gating.
 */
export function MessageMetrics({ message }: { message: Message }) {
  const line = buildMetricsString(message);
  if (line === null) return null;

  return <span className="spark-action-reveal ml-auto font-mono text-xs text-[#88857d]">{line}</span>;
}
