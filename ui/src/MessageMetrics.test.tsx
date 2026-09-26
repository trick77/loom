import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { MessageMetrics } from "./MessageMetrics";
import type { Message } from "./api";
import { resetModelInfoForTest } from "./api/model";

afterEach(() => {
  vi.unstubAllGlobals();
  resetModelInfoForTest();
});

function assistant(extra: Partial<Message>): Message {
  return {
    id: "m1",
    threadId: "t1",
    role: "assistant",
    content: "hi",
    createdAt: "2026-05-31T14:32:00Z",
    ...extra,
  };
}

test("renders nothing without renderable metrics", () => {
  // The assistant answer shows metrics only (no leading time), so with no token
  // metrics there is nothing to render.
  const { container } = render(
    <MessageMetrics message={assistant({ completionTokens: 100 })} />,
  );
  expect(container).toBeEmptyDOMElement();
});

test("renders the metrics line when data is present, leading with a stored reasoning effort", () => {
  render(
    <MessageMetrics
      message={assistant({
        model: "glm-5.3-flash",
        reasoningEffort: "high",
        durationMs: 5000,
        promptTokens: 10,
        completionTokens: 500,
        totalTokens: 510,
      })}
    />,
  );
  // No leading time on the answer; the line leads with the reasoning-effort level (never the model name).
  expect(screen.getByText(/^high · 5s · ↑/)).toBeInTheDocument();
});

test("shows the context % against the window /api/model reports", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ model: "glm-5.3-flash", contextWindow: 200_000 }),
    }),
  );
  render(
    <MessageMetrics
      message={assistant({
        durationMs: 5000,
        completionTokens: 500,
        contextTokens: 50_000,
      })}
    />,
  );
  // 50 000 / 200 000 = 25 %: the denominator is the backend's, not a UI constant.
  expect(await screen.findByText(/25\s%$/)).toBeInTheDocument();
});
