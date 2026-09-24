import { useEffect } from "react";

// useOutsidePointerDown calls onOutside when a pointer goes down anywhere
// isInside does not claim, while active. Every menu and popover used to carry
// its own copy of this listener with a slightly different predicate.
export function useOutsidePointerDown(
  active: boolean,
  isInside: (target: EventTarget | null) => boolean,
  onOutside: () => void,
): void {
  useEffect(() => {
    if (!active) return;
    function handlePointerDown(event: PointerEvent) {
      if (isInside(event.target)) return;
      onOutside();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [active, isInside, onOutside]);
}

// useOutsideRefPointerDown is the common case: the pointer landed outside the
// element the ref points at. A non-node target or an unmounted element counts
// as inside, so a menu is never closed by the click that is mounting it. The
// ref is read inside the listener, never during render.
export function useOutsideRefPointerDown(
  active: boolean,
  ref: { current: Node | null },
  onOutside: () => void,
): void {
  useEffect(() => {
    if (!active) return;
    function handlePointerDown(event: PointerEvent) {
      const target = event.target;
      if (!(target instanceof Node) || (ref.current?.contains(target) ?? true))
        return;
      onOutside();
    }
    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [active, ref, onOutside]);
}

// insideSelector claims everything under an ancestor matching selector.
export function insideSelector(
  selector: string,
): (target: EventTarget | null) => boolean {
  return (target) =>
    !(target instanceof Element) || target.closest(selector) !== null;
}
