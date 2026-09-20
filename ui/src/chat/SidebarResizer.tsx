import { useLayoutEffect, useRef, useState } from "react";

export const SIDEBAR_MIN = 280;
export const SIDEBAR_MAX = 520;
export const SIDEBAR_DEFAULT = 362;

const STORAGE_KEY = "loom:sidebar-width";
const STEP = 16; // px per arrow key
const DOUBLE_TAP_MS = 350;
const DRAG_SLOP = 3; // px of travel before a tap counts as a drag

/**
 * Bounds only. The viewport cap lives in the CSS clamp on --ui-sidebar-w, so that a
 * narrow window never rewrites the stored preference: clamping here and writing the
 * result back would lose the user's number the first time a tablet turns to portrait.
 */
export function clampSidebar(width: number): number {
  return Math.min(Math.max(Math.round(width), SIDEBAR_MIN), SIDEBAR_MAX);
}

/**
 * Private mode throws on storage access, and a sidebar at the default beats a blank
 * page. A hand-edited value is clamped rather than trusted.
 */
export function storedSidebarWidth(): number {
  let raw: string | null = null;
  try {
    raw = localStorage.getItem(STORAGE_KEY);
  } catch {
    return SIDEBAR_DEFAULT;
  }
  const parsed = Number(raw);
  return raw !== null && Number.isFinite(parsed) && parsed > 0
    ? clampSidebar(parsed)
    : SIDEBAR_DEFAULT;
}

function rememberSidebarWidth(width: number) {
  try {
    localStorage.setItem(STORAGE_KEY, String(width));
  } catch {
    // Ignore storage failures (private mode); the in-memory width still applies.
  }
}

/** The sidebar's on-screen width, which the CSS clamp may hold below the preference. */
function renderedWidth(fallback: number): number {
  // .ui-sidebar-text is the nav sidebar specifically: the Sources drawer is an
  // <aside> too, so a bare tag selector would measure the wrong box.
  const el = document.querySelector(".ui-sidebar-text");
  const width = el?.getBoundingClientRect().width ?? 0;
  return width > 0 ? width : fallback; // jsdom lays nothing out
}

/** Write the width to the DOM only. The CSS clamp on --ui-sidebar-w caps the viewport. */
function paint(width: number) {
  document.documentElement.style.setProperty("--ui-sidebar-pref", width + "px");
}

/**
 * Drag handle on the expanded sidebar's right edge, ported from ../transmission-ui.
 *
 * The width lives here and in one CSS variable, not in ThreadShell's state: the shell
 * re-renders on every streamed token, and a width held up there would be rewritten
 * mid-drag and snap the edge back. Nothing else reads the number, so this component
 * owns it end to end and writes localStorage once, on release.
 *
 * Pointer Events throughout, so mouse, trackpad, pencil and finger take one code
 * path. Reset is a double-tap read from pointer timestamps rather than onDoubleClick:
 * Safari only synthesises dblclick from a double-tap under conditions we would rather
 * not depend on, and a tablet has no other way back to the default width.
 */
export function SidebarResizer() {
  const [width, setWidth] = useState(storedSidebarWidth);
  const lastDown = useRef(-Infinity);
  // The live width during a drag. State alone is not enough: the pointerup handler
  // closes over the width of the render that ran when the drag began, so committing
  // that would snap the sidebar back to its pre-drag width.
  const live = useRef(width);
  const moved = useRef(false);
  const active = useRef<number | null>(null); // the pointer that owns the drag
  const start = useRef({ x: 0, width: 0 }); // grab point, so the edge does not jump

  // Layout, not effect: an effect paints after the first frame, so the sidebar would
  // flash at the default width before the stored preference landed.
  useLayoutEffect(() => {
    paint(width);
    // Mount only. Every later change paints itself through commit().
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  /**
   * `next` is a width the user just expressed on screen, so it is the rendered
   * edge. The preference may be wider: the CSS clamp caps --ui-sidebar-w at 40vw,
   * and a narrow window must not quietly spend the user's stored number. Widening
   * past the cap therefore keeps the larger preference, while any narrowing is
   * taken at face value because that is a deliberate act.
   */
  function commit(
    next: number,
    intent: "narrow" | "widen" | "exact" = "exact",
  ) {
    live.current = next;
    setWidth(next);
    paint(next);
    rememberSidebarWidth(
      intent === "widen" ? Math.max(next, storedSidebarWidth()) : next,
    );
  }

  function onPointerDown(event: React.PointerEvent<HTMLDivElement>) {
    if (event.pointerType === "mouse" && event.button !== 0) return;
    if (active.current !== null) return; // a drag runs; ignore a second finger
    const now = performance.now();
    if (now - lastDown.current < DOUBLE_TAP_MS) {
      lastDown.current = -Infinity;
      commit(SIDEBAR_DEFAULT);
      return;
    }
    lastDown.current = now;
    active.current = event.pointerId;
    // Seed from the RENDERED width, not the preference: --ui-sidebar-w is clamped to
    // 40vw, so on a narrow window the two diverge and an offset drag would spend
    // that difference moving nothing.
    start.current = { x: event.clientX, width: renderedWidth(live.current) };
    moved.current = false;
    event.preventDefault();
    // preventDefault suppresses the compatibility mousedown, and with it the focus it
    // would have given us. Focus explicitly, or the arrow keys do nothing until the
    // user tabs to the handle.
    event.currentTarget.focus();
    event.currentTarget.setPointerCapture?.(event.pointerId); // jsdom has none
    // The grid carries transition-[grid-template-columns]; without this the column
    // animates behind every frame and the edge lags the pointer.
    document.body.classList.add("resizing");
  }

  function onPointerMove(event: React.PointerEvent<HTMLDivElement>) {
    if (active.current !== event.pointerId) return;
    // Below the slop this is still a tap: a touch screen emits a pixel of jitter
    // during one, and treating that as a drag both nudged the edge and disarmed the
    // double-tap reset, the only way a finger has back to the default width.
    const dx = event.clientX - start.current.x;
    if (!moved.current && Math.abs(dx) < DRAG_SLOP) return;
    moved.current = true;
    // Offset from the grab point, not the raw clientX: grabbing the handle off-centre
    // would otherwise snap the border to the pointer by up to half the hit area.
    const next = clampSidebar(start.current.width + dx);
    live.current = next;
    setWidth(next);
    paint(next);
  }

  function onPointerUp(event: React.PointerEvent<HTMLDivElement>) {
    if (active.current !== event.pointerId) return;
    active.current = null;
    document.body.classList.remove("resizing");
    // A cancel before the slop is not a tap the user made: leaving the window
    // armed would swallow their next deliberate grab as a double-tap reset.
    if (event.type === "pointercancel") lastDown.current = -Infinity;
    // Commit before releasing capture: releasePointerCapture throws NotFoundError
    // when the pointer is already gone, which is exactly the pointercancel case, and
    // the throw would skip the commit and lose the drag.
    // A completed drag must not arm the double-tap window either: re-grabbing the
    // handle within it to fine-tune would snap the width back to the default.
    if (moved.current) {
      lastDown.current = -Infinity;
      commit(
        live.current,
        live.current >= start.current.width ? "widen" : "narrow",
      );
    }
    try {
      event.currentTarget.releasePointerCapture?.(event.pointerId);
    } catch {
      // Already gone; capture is released implicitly anyway.
    }
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    // Seeded from the RENDERED width for the same reason the drag is: with a
    // preference above the 40vw cap, stepping the preference would walk a number
    // nobody can see and announce it through aria-valuenow, while the edge stood
    // still for several presses.
    const from = renderedWidth(live.current);
    if (event.key === "ArrowLeft") commit(clampSidebar(from - STEP), "narrow");
    else if (event.key === "ArrowRight")
      commit(clampSidebar(from + STEP), "widen");
    else if (event.key === "Home") commit(SIDEBAR_DEFAULT);
    else return;
    event.preventDefault();
  }

  return (
    <div
      // Only from md, where the sidebar is the layout. Below it the sidebar is an
      // off-canvas drawer and this would drag an edge nobody can see.
      className="ui-sidebar-resizer absolute inset-y-0 z-30 hidden w-2.5 cursor-col-resize touch-none select-none md:block"
      style={{ left: "calc(var(--ui-sidebar-w) - 7px)" }}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize sidebar"
      aria-valuenow={width}
      aria-valuemin={SIDEBAR_MIN}
      aria-valuemax={SIDEBAR_MAX}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      onKeyDown={onKeyDown}
    />
  );
}
