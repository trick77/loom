import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { MessageMetrics } from "./MessageMetrics";
import type { Message } from "./api";

function assistant(extra: Partial<Message>): Message {
  return { id: "m1", threadId: "t1", role: "assistant", content: "hi", createdAt: "2026-05-31T14:32:00Z", ...extra };
}

test("renders nothing without renderable metrics", () => {
  // The assistant answer shows metrics only (no leading time), so with no token
  // metrics there is nothing to render.
  const { container } = render(<MessageMetrics message={assistant({ completionTokens: 100 })} />);
  expect(container).toBeEmptyDOMElement();
});

test("renders the metrics line when data is present, leading with the reasoning effort", () => {
  render(
    <MessageMetrics
      message={assistant({ model: "mimo", reasoningEffort: "high", durationMs: 5000, promptTokens: 10, completionTokens: 500, totalTokens: 510 })}
    />,
  );
  // No leading time on the answer; the line leads with the reasoning-effort level (never the model name).
  expect(screen.getByText(/^high · 5s · ↑/)).toBeInTheDocument();
});
