import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";
import { MessageMetrics } from "./MessageMetrics";
import { formatMessageTime } from "./metrics";
import type { Message } from "./api";

function assistant(extra: Partial<Message>): Message {
  return { id: "m1", threadId: "t1", role: "assistant", content: "hi", createdAt: "2026-05-31T14:32:00Z", ...extra };
}

test("renders the message time even without renderable metrics", () => {
  const message = assistant({ completionTokens: 100 });
  render(<MessageMetrics message={message} />);
  // No token metrics -> only the message's own time (HH:MM) shows.
  expect(screen.getByText(formatMessageTime(message.createdAt))).toBeInTheDocument();
});

test("renders the status line when data is present, leading with the time then the reasoning effort", () => {
  const message = assistant({ model: "mimo", reasoningEffort: "high", durationMs: 5000, promptTokens: 10, completionTokens: 500, totalTokens: 510 });
  render(<MessageMetrics message={message} />);
  // The line leads with the message time, then the reasoning-effort level (never the model name).
  const time = formatMessageTime(message.createdAt);
  expect(screen.getByText(new RegExp(`^${time} · high · 5s · ↑`))).toBeInTheDocument();
});
