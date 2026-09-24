import { fireEvent, renderHook } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import { useEscapeKey } from "./useEscapeKey";

function pressEscape() {
  fireEvent.keyDown(window, { key: "Escape" });
}

test("only the topmost handler receives Escape", () => {
  const stream = vi.fn();
  const lightbox = vi.fn();
  renderHook(() => useEscapeKey(stream));
  const top = renderHook(() => useEscapeKey(lightbox));

  pressEscape();
  expect(lightbox).toHaveBeenCalledTimes(1);
  expect(stream).not.toHaveBeenCalled();

  top.unmount();
  pressEscape();
  expect(stream).toHaveBeenCalledTimes(1);
});

test("an inactive handler is skipped and the latest handler is used", () => {
  const first = vi.fn();
  const second = vi.fn();
  const hook = renderHook(
    ({ fn, active }: { fn: () => void; active: boolean }) =>
      useEscapeKey(fn, { active }),
    { initialProps: { fn: first, active: false } },
  );

  pressEscape();
  expect(first).not.toHaveBeenCalled();

  hook.rerender({ fn: second, active: true });
  pressEscape();
  expect(second).toHaveBeenCalledTimes(1);
  expect(first).not.toHaveBeenCalled();
  hook.unmount();
});

test("a bottom handler yields to surfaces mounted before it", () => {
  const modal = vi.fn();
  const stop = vi.fn();
  const modalHook = renderHook(() => useEscapeKey(modal));
  const stopHook = renderHook(() => useEscapeKey(stop, { bottom: true }));

  pressEscape();
  expect(modal).toHaveBeenCalledTimes(1);
  expect(stop).not.toHaveBeenCalled();

  modalHook.unmount();
  pressEscape();
  expect(stop).toHaveBeenCalledTimes(1);
  stopHook.unmount();
});

test("the keypress is consumed", () => {
  const hook = renderHook(() => useEscapeKey(vi.fn()));
  const event = new KeyboardEvent("keydown", {
    key: "Escape",
    cancelable: true,
  });
  window.dispatchEvent(event);
  expect(event.defaultPrevented).toBe(true);
  hook.unmount();
});
