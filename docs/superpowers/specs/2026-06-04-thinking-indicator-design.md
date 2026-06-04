# Thinking Indicator Design

## Goal

Make Spark's assistant waiting state consistent across plain waiting, tool activity, streamed reasoning, and final answer startup.

## Current Behavior

The chat UI currently renders three different waiting surfaces:

- A three-dot bouncing indicator while waiting for the first assistant output.
- A separate tool activity panel during tool calls.
- A `Thinking...` / `Thinking` reasoning panel only after reasoning deltas arrive.

During tool-heavy turns, the generic waiting indicator can disappear before reasoning or content is visible, leaving the user unsure whether the backend is still active.

## Approved Design

Use one persistent thinking block for an active assistant turn.

- Show the thinking block immediately after the user message is submitted and before any reasoning, tool event, artifact, or answer text arrives.
- Use the label `Thinking`, not dots and not `Thinking...`.
- Animate the `Thinking` word with a slow shimmer sweep.
- The shimmer cycle is explicit: about 4.5 seconds total, about 3 seconds of visible sweep, then about 1.5 seconds of pause.
- The shimmer uses a non-repeating overlay that moves only just beyond the word edges, avoiding the previous long invisible travel and avoiding a final `g` flash at loop reset.
- When tool events arrive, keep the thinking block visible and render tool activity inside the same assistant-turn area below the label.
- When reasoning deltas arrive, add reasoning into the same block instead of replacing the waiting indicator with a different component.
- When the final assistant answer starts streaming, stop the active animation but keep any reasoning/tool details visible until the finalized assistant message replaces the transient stream.
- Stored assistant messages keep the existing collapsed `Thinking` panel for persisted reasoning, with a non-animated completed state.

## Out Of Scope

- Backend SSE changes.
- Tool execution semantics.
- Message persistence changes.
- Broader chat typography or layout changes.

## Verification

- Frontend tests cover that an active `Thinking` block appears before assistant output, remains present during tool calls, receives streamed reasoning in the same block, and disappears when the answer starts streaming.
- Existing tests for persisted reasoning and completed tool activity continue to pass.
