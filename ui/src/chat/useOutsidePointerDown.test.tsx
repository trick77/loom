import { fireEvent, renderHook } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import {
  insideSelector,
  useOutsidePointerDown,
  useOutsideRefPointerDown,
} from "./useOutsidePointerDown";

test("a pointer down outside the ref fires, inside does not, inactive never", () => {
  const inside = document.createElement("div");
  document.body.append(inside);
  const ref = { current: inside };
  const onOutside = vi.fn();
  const hook = renderHook(
    ({ active }: { active: boolean }) =>
      useOutsideRefPointerDown(active, ref, onOutside),
    { initialProps: { active: true } },
  );

  fireEvent.pointerDown(inside);
  expect(onOutside).not.toHaveBeenCalled();
  fireEvent.pointerDown(document.body);
  expect(onOutside).toHaveBeenCalledTimes(1);

  hook.rerender({ active: false });
  fireEvent.pointerDown(document.body);
  expect(onOutside).toHaveBeenCalledTimes(1);
  hook.unmount();
  inside.remove();
});

test("an unmounted ref element counts as inside", () => {
  const onOutside = vi.fn();
  const hook = renderHook(() =>
    useOutsideRefPointerDown(true, { current: null }, onOutside),
  );
  fireEvent.pointerDown(document.body);
  expect(onOutside).not.toHaveBeenCalled();
  hook.unmount();
});

test("useOutsidePointerDown with insideSelector closes on clicks outside the root", () => {
  const root = document.createElement("div");
  root.setAttribute("data-menu-root", "");
  const child = document.createElement("span");
  root.append(child);
  document.body.append(root);
  const onOutside = vi.fn();
  const isInside = insideSelector("[data-menu-root]");
  const hook = renderHook(() =>
    useOutsidePointerDown(true, isInside, onOutside),
  );
  fireEvent.pointerDown(child);
  expect(onOutside).not.toHaveBeenCalled();
  fireEvent.pointerDown(document.body);
  expect(onOutside).toHaveBeenCalledTimes(1);
  hook.unmount();
  root.remove();
});
