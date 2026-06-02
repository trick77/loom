import type { Message } from "./api";
import { buildMetricsString } from "./metrics";

/**
 * Renders the stats line for an assistant message, right-aligned via `ml-auto`
 * within the actions row. Hover-only: visibility comes from the parent's JS hover
 * state (`visible`), toggling a plain opacity class. Not CSS :hover/group-hover —
 * that sticks "on" in Safari after the first hover.
 */
export function MessageMetrics({ message, visible }: { message: Message; visible: boolean }) {
  const line = buildMetricsString(message);
  if (line === null) return null;

  return (
    <span
      className={`ml-auto font-mono text-xs text-[#88857d] transition-opacity duration-300 ${
        visible ? "opacity-100" : "opacity-0"
      }`}
    >
      {line}
    </span>
  );
}
