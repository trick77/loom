import type { Message } from "./api";
import { buildMetricsString } from "./metrics";

/**
 * Renders the stats line for an assistant message, right-aligned via `ml-auto`
 * within the actions row. Always visible.
 */
export function MessageMetrics({ message }: { message: Message }) {
  const line = buildMetricsString(message);
  if (line === null) return null;

  // Color matches the action icons to the left (idle #858178) so the row reads as
  // one muted cluster; the brighter reasoning-title cream was too loud here.
  return <span className="ml-auto font-sans text-[0.8125rem] text-[#858178]">{line}</span>;
}
