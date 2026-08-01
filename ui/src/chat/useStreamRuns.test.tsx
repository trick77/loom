import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useStreamRuns } from "./useStreamRuns";
import { threadRunKey } from "./streamRuns";

const KEY = threadRunKey("t1");

describe("useStreamRuns patch", () => {
  it("replaces fields when given a plain object", () => {
    const { result } = renderHook(() => useStreamRuns());

    act(() => result.current.begin(KEY, new AbortController()));
    act(() => result.current.patch(KEY, { toolPending: true }));

    expect(result.current.runs[KEY].toolPending).toBe(true);
  });

  // The updater form exists so a patch can merge rather than replace: the two
  // source snapshots each carry only their own kind of source, and replacing the
  // whole list on either would drop the other.
  it("derives the patch from the run when given a function", () => {
    const { result } = renderHook(() => useStreamRuns());
    const doc = {
      documentId: "d1",
      filename: "runbook.md",
      snippet: "",
      score: 1,
      index: 1,
    };
    const page = {
      documentId: "",
      filename: "Kubernetes",
      snippet: "",
      score: 1,
      index: 2,
      url: "https://kubernetes.io",
    };

    act(() => result.current.begin(KEY, new AbortController()));
    act(() => result.current.patch(KEY, { sources: [doc] }));
    act(() =>
      result.current.patch(KEY, (run) => ({ sources: [...run.sources, page] })),
    );

    expect(result.current.runs[KEY].sources.map((s) => s.index)).toEqual([
      1, 2,
    ]);
  });

  // patchRun drops patches for an absent key, so the updater must still receive a
  // usable run rather than throwing on undefined.
  it("does not throw when the key has no run", () => {
    const { result } = renderHook(() => useStreamRuns());

    act(() =>
      result.current.patch(KEY, (run) => ({ sources: [...run.sources] })),
    );

    expect(result.current.runs[KEY]).toBeUndefined();
  });
});
