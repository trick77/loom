import { useCallback, useLayoutEffect, useRef, useState } from "react";

// Finds the nearest scrollable ancestor so we can tell whether a dropdown would
// overflow the visible area (e.g. the sidebar's scroll <nav>, or a page's
// overflow-y-auto column) rather than the whole viewport.
function nearestScrollParent(el: HTMLElement): HTMLElement | null {
  let parent = el.parentElement;
  while (parent !== null) {
    const overflowY = getComputedStyle(parent).overflowY;
    if (overflowY === "auto" || overflowY === "scroll") return parent;
    parent = parent.parentElement;
  }
  return null;
}

// Shared open-direction logic for the absolute context menus (thread / project /
// artifact actions). They render inside overflow-clipped scroll containers, so a
// menu opened on a row near the bottom would drop down into (and behind) whatever
// sits below - the account bar, the end of a list. When there isn't room below,
// flip the menu upward instead.
//
// Returns a ref to attach to the menu container and the vertical placement
// classes to apply. Callers keep supplying only horizontal placement.
export function useMenuPlacement(): {
  menuRef: React.RefObject<HTMLDivElement | null>;
  dropUp: boolean;
  verticalClass: string;
} {
  const menuRef = useRef<HTMLDivElement>(null);
  const [dropUp, setDropUp] = useState(false);

  // measure decides the open direction from the live geometry: el.offsetHeight is
  // the real (variable) menu height, offsetParent is the positioned anchor (the
  // `relative` row/wrapper), and the nearest scroll container gives the visible
  // bounds the menu must fit within.
  const measure = useCallback(() => {
    const el = menuRef.current;
    const anchor = el?.offsetParent as HTMLElement | null;
    if (el === null || anchor === null) return;
    const scroller = nearestScrollParent(el);
    const bounds = scroller?.getBoundingClientRect();
    const topLimit = Math.max(bounds?.top ?? 0, 0);
    const bottomLimit = Math.min(
      bounds?.bottom ?? window.innerHeight,
      window.innerHeight,
    );
    const anchorRect = anchor.getBoundingClientRect();
    const menuHeight = el.offsetHeight + 4;
    const spaceBelow = bottomLimit - anchorRect.bottom;
    const spaceAbove = anchorRect.top - topLimit;
    setDropUp(spaceBelow < menuHeight && spaceAbove > spaceBelow);
  }, []);

  // Measure before paint so the menu never flashes downward first, then keep the
  // direction correct while it stays open: scrolling the list/sidebar or resizing
  // the window changes the anchor's position relative to the visible bounds.
  useLayoutEffect(() => {
    measure();
    // Capture-phase scroll catches any ancestor scroll container, not just window.
    window.addEventListener("scroll", measure, true);
    window.addEventListener("resize", measure);
    return () => {
      window.removeEventListener("scroll", measure, true);
      window.removeEventListener("resize", measure);
    };
  }, [measure]);

  return {
    menuRef,
    dropUp,
    verticalClass: dropUp ? "top-auto bottom-full mb-1" : "top-full mt-1",
  };
}
