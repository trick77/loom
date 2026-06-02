import type { Message } from "./api";
import { buildMetricsString } from "./metrics";

/**
 * Renders the stats line for an assistant message, right-aligned via `ml-auto`
 * within the actions row. Hover-only via AnythingLLM's exact CSS group-hover
 * classes (verified to work in Safari): hidden at md+ until the message wrapper
 * is hovered.
 */
export function MessageMetrics({ message }: { message: Message }) {
  const line = buildMetricsString(message);
  if (line === null) return null;

  return (
    <span className="ml-auto font-mono text-xs text-[#88857d] md:opacity-0 md:group-hover:opacity-100 transition-all duration-300">
      {line}
    </span>
  );
}
