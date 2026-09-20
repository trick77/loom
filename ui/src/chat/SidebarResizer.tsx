import { useEffect, useLayoutEffect, useRef, useState } from "react";

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
    // Private mode or a full quota. The width holds for this mount, but collapsing
    // the sidebar unmounts the handle and the next expand re-reads storage, so the
    // drag is quietly lost then. Better than throwing on a resize.
  }
}

/**
 * What the CSS clamp on --ui-sidebar-w resolves the preference to, computed rather
 * than measured. The shell grid transitions grid-template-columns for 200ms, so the
 * aside's own box is mid-animation after every column change: seeding a drag or an
 * arrow key from it read a width in flight, which made repeated presses compound to
 * less than a step and let a grab during the expand animation jump the edge.
 * Mirrors `clamp(280px, pref, min(520px, 40vw))` exactly.
 */
export function displayedWidth(preference: number): number {
  const vw = document.documentElement.clientWidth || 0;
  const cap = vw > 0 ? Math.min(SIDEBAR_MAX, 0.4 * vw) : SIDEBAR_MAX;
  return Math.round(Math.max(SIDEBAR_MIN, Math.min(preference, cap)));
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
  // Grab point, so the edge does not jump to the pointer. `width` is where the edge
  // sits on screen and `preference` the number behind it; under the 40vw cap they
  // differ, and the drag picks between them by direction.
  const start = useRef({ x: 0, width: 0, preference: 0 });

  // Layout, not effect: an effect paints after the first frame, so the sidebar would
  // flash at the default width before the stored preference landed.
  useLayoutEffect(() => {
    paint(width);
    // Mount only. Every later change paints itself through commit().
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Collapsing the sidebar unmounts this component, and a collapse during a drag
  // (a second finger on the hide button) would leave body.resizing behind. That
  // class disables every transition in the app and pins the cursor to col-resize,
  // and nothing would clear it until some later drag happened to finish.
  useEffect(
    () => () => {
      active.current = null;
      document.body.classList.remove("resizing");
    },
    [],
  );

  // aria-valuenow reports the DISPLAYED width, which the 40vw cap moves with the
  // window. CSS repaints the edge on a resize but React does not re-render, so
  // without this the separator kept announcing the width from the last render:
  // wrong as the preference AND wrong as the edge. Only the rendered output
  // depends on this, so a bare re-render is the whole job.
  const [, bumpOnResize] = useState(0);
  useEffect(() => {
    const onResize = () => bumpOnResize((n) => n + 1);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  /**
   * One number throughout: the PREFERENCE. The 40vw cap is presentation, applied by
   * the CSS clamp on screen and reported through aria-valuenow, and is never written
   * back over the preference. Keeping the cap out of the stored and painted value is
   * what lets a 520 set on a desktop survive a session spent in portrait.
   */
  function commit(next: number) {
    live.current = next;
    setWidth(next);
    paint(next);
    rememberSidebarWidth(next);
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
    start.current = {
      x: event.clientX,
      width: displayedWidth(live.current),
      preference: live.current,
    };
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
    // Direction decides which number the travel applies to, because under the cap
    // the preference sits above the visible edge.
    //   widening: from the PREFERENCE, so a nudge does not overwrite a stored 520
    //             with the capped number the screen happens to be showing.
    //   narrowing: from the EDGE, which is what the finger is actually on. Adding
    //             the travel to the preference instead gave a dead zone as wide as
    //             the gap: a 40px pull moved nothing and still wrote 480 back.
    const from = dx < 0 ? start.current.width : start.current.preference;
    const next = clampSidebar(from + dx);
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
      commit(live.current);
    }
    try {
      event.currentTarget.releasePointerCapture?.(event.pointerId);
    } catch {
      // Already gone; capture is released implicitly anyway.
    }
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    // Mirrors the drag: narrowing steps the edge the user can see, so the first
    // press always moves something; widening steps the preference, so travel above
    // the cap is kept for the window that can show it. Above the cap a widening
    // press therefore moves no pixels, the same bargain the drag makes, which is
    // why aria-valuenow reports the displayed width rather than the preference.
    // Both computed, never measured: the aside's box is still animating for 200ms
    // after a column change, and two quick presses would compound against it.
    const shown = displayedWidth(live.current);
    if (event.key === "ArrowLeft") commit(clampSidebar(shown - STEP));
    else if (event.key === "ArrowRight")
      commit(clampSidebar(live.current + STEP));
    else if (event.key === "Home") commit(SIDEBAR_DEFAULT);
    else return;
    event.preventDefault();
  }

  return (
    <div
      // Only from md, where the sidebar is the layout. Below it the sidebar is an
      // off-canvas drawer and this would drag an edge nobody can see.
      // 1px inside the border, not 7px: the sidebar's own ::-webkit-scrollbar is
      // 8px of track down that edge, and covering it meant that on Windows, Linux,
      // or macOS set to always-show scrollbars, reaching for the thumb resized the
      // pane instead of scrolling it. The single pixel keeps the border itself
      // grabbable, since the visible line is what people aim at.
      className="ui-sidebar-resizer absolute inset-y-0 z-30 hidden w-2.5 cursor-col-resize touch-none select-none md:block"
      style={{ left: "calc(var(--ui-sidebar-w) - 1px)" }}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize sidebar"
      // The DISPLAYED width, not the preference: the separator is where the clamp
      // puts it, and announcing 520 while it sits at 440 describes a sidebar that
      // is not on screen. The max moves with the cap for the same reason.
      aria-valuenow={displayedWidth(width)}
      aria-valuemin={SIDEBAR_MIN}
      aria-valuemax={displayedWidth(SIDEBAR_MAX)}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      onKeyDown={onKeyDown}
    />
  );
}
