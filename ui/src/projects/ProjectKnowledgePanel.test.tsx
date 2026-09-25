import "@testing-library/jest-dom/vitest";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import * as api from "../api";
import { ProjectKnowledgePanel } from "./ProjectKnowledgePanel";

function doc(filename: string, status: api.Document["status"]): api.Document {
  return {
    id: `doc-${filename}`,
    projectId: "p",
    filename,
    mime: "text/plain",
    sizeBytes: 12,
    status,
    createdAt: "2026-05-30T00:00:00Z",
  } as unknown as api.Document;
}

beforeEach(() => vi.restoreAllMocks());
afterEach(() => vi.useRealTimers());

// Switching projects while the previous list was still loading let that
// answer land on top of the new project's documents.
test("a list that answers after the project changed is ignored", async () => {
  let resolveFirst: (docs: api.Document[]) => void = () => {};
  vi.spyOn(api, "listDocuments").mockImplementation((projectId) =>
    projectId === "p1"
      ? new Promise<api.Document[]>((resolve) => {
          resolveFirst = resolve;
        })
      : Promise.resolve([doc("b.txt", "embedded")]),
  );

  const view = render(<ProjectKnowledgePanel projectId="p1" />);
  view.rerender(<ProjectKnowledgePanel projectId="p2" />);
  expect(await screen.findByText("b.txt")).toBeInTheDocument();

  await act(async () => {
    resolveFirst([doc("a.txt", "embedded")]);
    await Promise.resolve();
  });
  expect(screen.queryByText("a.txt")).toBeNull();
  expect(screen.getByText("b.txt")).toBeInTheDocument();
});

test("polling backs off while a document is still being indexed", async () => {
  vi.useFakeTimers();
  const list = vi
    .spyOn(api, "listDocuments")
    .mockImplementation(async () => [doc("x.txt", "extracting")]);

  render(<ProjectKnowledgePanel projectId="p1" />);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
  expect(list).toHaveBeenCalledTimes(1);

  for (const [advance, calls] of [
    [2000, 2],
    [5000, 3],
    [10000, 4],
    [5000, 4], // the 10s step repeats: nothing fires halfway through it
    [5000, 5],
  ] as const) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(advance);
    });
    expect(list).toHaveBeenCalledTimes(calls);
  }
});
