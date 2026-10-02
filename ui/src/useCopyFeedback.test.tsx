import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

import { useCopyFeedback } from "./useCopyFeedback";

const writeText = vi.fn();

beforeEach(() => {
  vi.useFakeTimers();
  writeText.mockReset().mockResolvedValue(undefined);
  vi.stubGlobal("navigator", { clipboard: { writeText } });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

test("reports copied, then returns to idle after the reset delay", async () => {
  const hook = renderHook(() => useCopyFeedback(1500));
  expect(hook.result.current.status).toBe("idle");

  let ok = false;
  await act(async () => {
    ok = await hook.result.current.copy("hello");
  });
  expect(ok).toBe(true);
  expect(writeText).toHaveBeenCalledWith("hello");
  expect(hook.result.current.status).toBe("copied");

  act(() => vi.advanceTimersByTime(1499));
  expect(hook.result.current.status).toBe("copied");
  act(() => vi.advanceTimersByTime(1));
  expect(hook.result.current.status).toBe("idle");
});

test("a second copy restarts the delay instead of inheriting the first one's", async () => {
  const hook = renderHook(() => useCopyFeedback(1500));
  await act(async () => void (await hook.result.current.copy("a")));
  act(() => vi.advanceTimersByTime(1000));
  await act(async () => void (await hook.result.current.copy("b")));

  act(() => vi.advanceTimersByTime(1000));
  expect(hook.result.current.status).toBe("copied");
  act(() => vi.advanceTimersByTime(500));
  expect(hook.result.current.status).toBe("idle");
});

test("remembers which of several targets was copied", async () => {
  const hook = renderHook(() => useCopyFeedback<string>(1500));
  await act(async () => void (await hook.result.current.copy("url", "s1")));

  expect(hook.result.current.copiedKey).toBe("s1");
  act(() => vi.advanceTimersByTime(1500));
  expect(hook.result.current.copiedKey).toBeNull();
});

test("a rejected write returns false and leaves the feedback alone", async () => {
  writeText.mockRejectedValue(new Error("denied"));
  const hook = renderHook(() => useCopyFeedback(1500));

  let ok = true;
  await act(async () => {
    ok = await hook.result.current.copy("hello");
  });

  expect(ok).toBe(false);
  expect(hook.result.current.status).toBe("idle");
  expect(vi.getTimerCount()).toBe(0);
});

test("a missing clipboard counts as a failed write", async () => {
  vi.stubGlobal("navigator", {});
  const hook = renderHook(() => useCopyFeedback(1500));

  let ok = true;
  await act(async () => {
    ok = await hook.result.current.copy("hello");
  });

  expect(ok).toBe(false);
});

test("can show the failure for the same delay", async () => {
  writeText.mockRejectedValue(new Error("denied"));
  const hook = renderHook(() => useCopyFeedback(1200, { showFailure: true }));

  await act(async () => void (await hook.result.current.copy("hello")));
  expect(hook.result.current.status).toBe("failed");

  act(() => vi.advanceTimersByTime(1200));
  expect(hook.result.current.status).toBe("idle");
});

test("unmounting clears the pending reset", async () => {
  const hook = renderHook(() => useCopyFeedback(1500));
  await act(async () => void (await hook.result.current.copy("hello")));
  expect(vi.getTimerCount()).toBe(1);

  hook.unmount();

  expect(vi.getTimerCount()).toBe(0);
});

test("a write that settles after unmount starts no timer", async () => {
  let settle: () => void = () => {};
  writeText.mockReturnValue(
    new Promise<void>((resolve) => {
      settle = resolve;
    }),
  );
  const hook = renderHook(() => useCopyFeedback(1500));
  let pending: Promise<boolean> = Promise.resolve(false);
  act(() => {
    pending = hook.result.current.copy("hello");
  });

  hook.unmount();
  settle();
  await pending;

  expect(vi.getTimerCount()).toBe(0);
});
