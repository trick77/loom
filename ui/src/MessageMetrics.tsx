import type { Message } from "./api";
import { buildStatusLine, humanizeCategory } from "./metrics";

/**
 * Renders the status line at the end of a message, right-aligned via `ml-auto`
 * within the actions row. The line leads with the message's own time (HH:MM) and,
 * for assistant messages, continues with the metrics ("07:30 · high · 5s · …").
 * Messages without token metrics (e.g. the user's sent messages) show the time alone.
 * When the thread has a prompt-classifier category, a pill with the humanized label
 * sits to the left of the line.
 */
export function MessageMetrics({ message, category }: { message: Message; category?: string }) {
  const line = buildStatusLine(message);
  const pill =
    category !== undefined && category !== "" ? (
      <span className="inline-flex items-center rounded-full bg-[#363632] px-2 py-0.5 font-sans text-[0.75rem] leading-[1.45rem] text-[#d6d3ca]">
        {humanizeCategory(category)}
      </span>
    ) : null;

  if (pill === null && line === "") return null;

  // Status text color matches the action icons to the left (idle #858178) so the
  // row reads as one muted cluster; the pill keeps its own chip styling.
  return (
    <span className="ml-auto flex items-center gap-2">
      {pill}
      {line !== "" && (
        <span className="font-sans text-[0.75rem] leading-[1.45rem] text-[#858178]">{line}</span>
      )}
    </span>
  );
}
