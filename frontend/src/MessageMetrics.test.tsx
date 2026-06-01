import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { MessageMetrics, MetricsProvider, SHOW_METRICS_KEY } from "./MessageMetrics";
import type { Message } from "./api";

function assistant(extra: Partial<Message>): Message {
  return { id: "m1", threadId: "t1", role: "assistant", content: "hi", createdAt: "2026-05-31T14:32:00Z", ...extra };
}

// jsdom in this project ships only a partial localStorage, so back it with a Map.
function stubLocalStorage() {
  const store = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => (store.has(key) ? store.get(key)! : null),
    setItem: (key: string, value: string) => void store.set(key, String(value)),
    removeItem: (key: string) => void store.delete(key),
    clear: () => store.clear(),
  });
}

beforeEach(() => {
  stubLocalStorage();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test("renders nothing without renderable metrics", () => {
  const { container } = render(
    <MetricsProvider>
      <MessageMetrics message={assistant({ completionTokens: 100 })} />
    </MetricsProvider>,
  );
  expect(container).toBeEmptyDOMElement();
});

test("renders the metrics line when data is present", () => {
  render(
    <MetricsProvider>
      <MessageMetrics message={assistant({ model: "mimo", durationMs: 5000, promptTokens: 10, completionTokens: 500, totalTokens: 510 })} />
    </MetricsProvider>,
  );
  expect(screen.getByText(/mimo · 5\.0s \(100\.00 tok\/s\)/)).toBeInTheDocument();
});

test("clicking toggles the persisted always-show preference", () => {
  render(
    <MetricsProvider>
      <MessageMetrics message={assistant({ durationMs: 2000, completionTokens: 100 })} />
    </MetricsProvider>,
  );
  fireEvent.click(screen.getByRole("button"));
  expect(window.localStorage.getItem(SHOW_METRICS_KEY)).toBe("true");
  fireEvent.click(screen.getByRole("button"));
  expect(window.localStorage.getItem(SHOW_METRICS_KEY)).toBe("false");
});
