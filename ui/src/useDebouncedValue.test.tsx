import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { useDebouncedValue } from "./useDebouncedValue";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

test("follows the value only after it has been still for the delay", () => {
  const hook = renderHook(({ value }) => useDebouncedValue(value, 250), {
    initialProps: { value: "" },
  });
  expect(hook.result.current).toBe("");

  hook.rerender({ value: "a" });
  act(() => vi.advanceTimersByTime(200));
  hook.rerender({ value: "ab" });
  act(() => vi.advanceTimersByTime(200));
  expect(hook.result.current).toBe("");

  act(() => vi.advanceTimersByTime(50));
  expect(hook.result.current).toBe("ab");
});

test("unmounting drops the pending update", () => {
  const hook = renderHook(({ value }) => useDebouncedValue(value, 250), {
    initialProps: { value: "" },
  });
  hook.rerender({ value: "a" });

  hook.unmount();

  expect(vi.getTimerCount()).toBe(0);
});
