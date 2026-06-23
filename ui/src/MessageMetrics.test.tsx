import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { MessageMetrics } from "./MessageMetrics";
import type { Message } from "./api";

function assistant(extra: Partial<Message>): Message {
  return { id: "m1", threadId: "t1", role: "assistant", content: "hi", createdAt: "2026-05-31T14:32:00Z", ...extra };
}

test("renders nothing without renderable metrics", () => {
  const { container } = render(<MessageMetrics message={assistant({ completionTokens: 100 })} />);
  expect(container).toBeEmptyDOMElement();
});

test("renders the metrics line when data is present", () => {
  render(
    <MessageMetrics
      message={assistant({ model: "mimo", durationMs: 5000, promptTokens: 10, completionTokens: 500, totalTokens: 510 })}
    />,
  );
  expect(screen.getByText(/^mimo · 5s · ↑/)).toBeInTheDocument();
});
