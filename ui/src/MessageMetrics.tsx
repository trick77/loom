import type { Message } from "./api";
import { contextWindowFor, modelNameFor } from "./api/model";
import { buildMetricsString, humanizeCategory } from "./metrics";
import { useModelInfo } from "./useModelInfo";

// Between the model name and the metrics: the trailing half of the line's own
// separator (metrics DOT_SEPARATOR), the flex gap standing in for the leading
// half. Built from char codes: a literal no-break space trips eslint.
const NAME_SEPARATOR = String.fromCharCode(0xb7, 0xa0, 0x20);

/**
 * Renders the metrics line for an assistant message, right-aligned via `ml-auto`
 * within the actions row. The assistant answer shows metrics only ("5s · ↑ … · ↓ …"),
 * with no leading time — the message time is surfaced on the user's own prompt
 * instead (see the alignRight branch in MessageActions). When the thread has a
 * prompt-classifier category, a pill with the humanized label sits to the left of
 * the metrics. The pill can show on its own before token metrics exist (line may be null).
 * The metrics line itself is desktop-only (`md` and up): on mobile it wraps and dominates
 * the action row, so it is hidden there while the pill keeps showing at every width.
 * When more than one model can answer, the answering model's name leads, at every
 * width: after a model switch, which model wrote what is otherwise invisible.
 */
export function MessageMetrics({
  message,
  category,
  threadCostNanoUsd,
}: {
  message: Message;
  category?: string;
  // The thread's cost through this message, for the Σ segment.
  threadCostNanoUsd?: number;
}) {
  // The window of the model that wrote this message: a thread can switch.
  const info = useModelInfo();
  const contextWindow = contextWindowFor(info, message.model);
  const line = buildMetricsString(message, threadCostNanoUsd, contextWindow);
  const modelName = modelNameFor(info, message.model);
  const pill =
    category !== undefined && category !== "" ? (
      <span className="inline-flex items-center rounded-full bg-[#363632] px-2 py-0.5 font-sans text-[0.75rem] leading-[1.45rem] text-[#d6d3ca]">
        {humanizeCategory(category)}
      </span>
    ) : null;

  if (pill === null && line === null && modelName === undefined) return null;

  // Metrics text color matches the action icons to the left (idle #858178) so the
  // row reads as one muted cluster; the pill keeps its own chip styling.
  return (
    <span className="ml-auto flex items-center gap-2">
      {pill}
      {modelName !== undefined && (
        <span className="font-sans text-[0.75rem] leading-[1.45rem] text-[#858178]">
          {modelName}
        </span>
      )}
      {line !== null && (
        <span className="hidden font-sans text-[0.75rem] leading-[1.45rem] text-[#858178] md:inline">
          {modelName !== undefined ? NAME_SEPARATOR + line : line}
        </span>
      )}
    </span>
  );
}
