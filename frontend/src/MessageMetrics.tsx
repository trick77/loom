import type { Message } from "./api";
import { buildMetricsString } from "./metrics";

/**
 * Renders the stats line for an assistant message, right-aligned via `ml-auto`
 * within the actions row. Hover-only (AnythingLLM-style): hidden by default and
 * revealed via the message wrapper's `group-hover` — same as the copy/retry
 * buttons. The loudspeaker stays always visible.
 */
export function MessageMetrics({ message }: { message: Message }) {
  const line = buildMetricsString(message);
  if (line === null) return null;

  return (
    <span className="ml-auto font-mono text-xs text-[#88857d] opacity-0 transition-all duration-300 group-hover:opacity-100">
      {line}
    </span>
  );
}
