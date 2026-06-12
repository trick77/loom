import { act, renderHook } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { appendReasoningDelta } from "../activityTrace";
import { useActivityTrace } from "./useActivityTrace";

describe("useActivityTrace", () => {
  test("update keeps state and ref in sync", () => {
    const { result } = renderHook(() => useActivityTrace());

    act(() => result.current.update((current) => appendReasoningDelta(current, "Hello")));

    expect(result.current.trace).toHaveLength(1);
    expect(result.current.trace[0]).toMatchObject({ type: "reasoning", content: "Hello" });
    // The ref mirrors state so streaming closures read the live value.
    expect(result.current.traceRef.current).toBe(result.current.trace);
  });

  test("clear empties the trace and drops the pending-tool bridge", () => {
    const { result } = renderHook(() => useActivityTrace());

    act(() => {
      result.current.update((current) => appendReasoningDelta(current, "Hi"));
      result.current.setToolPending(true);
    });
    expect(result.current.toolPending).toBe(true);

    act(() => result.current.clear());

    expect(result.current.trace).toEqual([]);
    expect(result.current.traceRef.current).toEqual([]);
    expect(result.current.toolPending).toBe(false);
  });
});
