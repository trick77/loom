import "@testing-library/jest-dom/vitest";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import type { Page } from "./api";
import { useInfiniteList } from "./useInfiniteList";

// A sentinel on screen: observe() reports it visible, as a browser does for
// an element that is in view when observed.
class VisibleSentinelObserver {
  constructor(
    private callback: (entries: Array<{ isIntersecting: boolean }>) => void,
  ) {}
  observe() {
    queueMicrotask(() => this.callback([{ isIntersecting: true }]));
  }
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

beforeEach(() => {
  vi.stubGlobal("IntersectionObserver", VisibleSentinelObserver);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function List({
  fetchPage,
  resetKey = "",
}: {
  fetchPage: (cursor: string | null) => Promise<Page<string>>;
  resetKey?: string;
}) {
  const { items, error, paged, sentinelRef } = useInfiniteList(fetchPage, [
    resetKey,
  ]);
  return (
    <>
      <ul>
        {items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </ul>
      {error !== null && <p role="alert">failed</p>}
      <p data-testid="paged">{paged ? "paged" : "whole"}</p>
      <div ref={sentinelRef} />
    </>
  );
}

test("a failed next page is not retried in a loop while the sentinel stays in view", async () => {
  const fetchPage = vi.fn(async (cursor: string | null) => {
    if (cursor === null) return { items: ["a"], nextCursor: "c2" };
    // A network failure lands in a later task, not the same microtask.
    await new Promise((resolve) => setTimeout(resolve, 1));
    throw new Error("offline");
  });
  render(<List fetchPage={fetchPage} />);
  expect(await screen.findByRole("alert")).toBeInTheDocument();

  // Let any re-observe loop run.
  for (let i = 0; i < 20; i++) {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }
  expect(
    fetchPage.mock.calls.filter(([cursor]) => cursor === "c2"),
  ).toHaveLength(1);
});

test("a page that fills nothing still loads the next one", async () => {
  const fetchPage = vi.fn(
    async (cursor: string | null): Promise<Page<string>> =>
      cursor === null
        ? { items: ["a"], nextCursor: "c2" }
        : { items: ["b"], nextCursor: null },
  );
  render(<List fetchPage={fetchPage} />);
  expect(await screen.findByText("b")).toBeInTheDocument();
});

test("paged tells a list that came in pages from one that came whole, until a reset", async () => {
  const fetchPage = vi.fn(
    async (cursor: string | null): Promise<Page<string>> =>
      cursor === null
        ? { items: ["a"], nextCursor: "c2" }
        : { items: ["b"], nextCursor: null },
  );
  const { rerender } = render(<List fetchPage={fetchPage} />);
  // Still paged once the last page is in.
  expect(await screen.findByText("b")).toBeInTheDocument();
  expect(screen.getByTestId("paged")).toHaveTextContent("paged");

  const whole = vi.fn(async (): Promise<Page<string>> => ({
    items: ["w"],
    nextCursor: null,
  }));
  rerender(<List fetchPage={whole} resetKey="other" />);
  expect(await screen.findByText("w")).toBeInTheDocument();
  expect(screen.getByTestId("paged")).toHaveTextContent("whole");
});
