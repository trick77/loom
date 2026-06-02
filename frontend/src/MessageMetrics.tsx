import type { Message } from "./api";
import { buildMetricsString } from "./metrics";

/**
 * Renders the stats line for an assistant message, right-aligned via `ml-auto`
 * within the actions row. Always visible.
 */
export function MessageMetrics({ message }: { message: Message }) {
  const line = buildMetricsString(message);
  if (line === null) return null;

  return <span className="ml-auto font-mono text-xs text-[#88857d]">{line}</span>;
}
