import type { Message } from "./api";
import { buildMetricsString, humanizeCategory } from "./metrics";

/**
 * Renders the stats line for an assistant message, right-aligned via `ml-auto`
 * within the actions row. When the thread has a prompt-classifier category, a pill
 * with the humanized label sits to the left of the metrics. The pill can show on
 * its own even before token metrics exist (the metrics line may be null).
 */
export function MessageMetrics({ message, category }: { message: Message; category?: string }) {
  const line = buildMetricsString(message);
  const pill =
    category !== undefined && category !== "" ? (
      <span className="inline-flex items-center rounded-full bg-[#46453f] px-2 py-0.5 font-sans text-[0.75rem] leading-[1.45rem] text-[#d6d3ca]">
        {humanizeCategory(category)}
      </span>
    ) : null;

  if (pill === null && line === null) return null;

  // Metrics text color matches the action icons to the left (idle #858178) so the
  // row reads as one muted cluster; the pill keeps its own chip styling.
  return (
    <span className="ml-auto flex items-center gap-2">
      {pill}
      {line !== null && (
        <span className="font-sans text-[0.75rem] leading-[1.45rem] text-[#858178]">{line}</span>
      )}
    </span>
  );
}
