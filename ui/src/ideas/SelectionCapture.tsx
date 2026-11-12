import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import { Icon } from "../chat/Icon";
import { useEscapeKey } from "../chat/useEscapeKey";
import { captureSaving } from "./IdeaCapture";
import {
  captureKey as keyOf,
  useIdeaCapture,
  type CaptureRequest,
} from "./IdeaCaptureContext";
import { canHover } from "./IdeaEditDialog";
import { capExcerpt } from "./ideaText";
import { selectionInAnswer, selectionTarget } from "./selectionTarget";

// SETTLE_MS is how long a touch selection must rest before the bar shows:
// it does not flicker while a handle is dragged.
export const SETTLE_MS = 350;

// The pill's height and gap above the selection, and the room it needs
// beside the screen's edges.
const PILL_H = 30;
const PILL_GAP = 6;
const EDGE = 8;
const PILL_W = 190;

type Shown =
  | { kind: "pill"; request: CaptureRequest; top: number; left: number }
  | { kind: "bar"; request: CaptureRequest; bottom: number | null };

// ownTarget: the event came from the pill or bar itself.
const ownTarget = (event: Event) =>
  event.target instanceof Element &&
  event.target.closest("[data-idea-select]") !== null;

const typing = (element: Element | null) =>
  element !== null &&
  (element.matches("input, textarea, select") ||
    (element instanceof HTMLElement && element.isContentEditable));

// KEYBOARD_PX: the visual viewport is shorter than the layout by more than
// this while an on-screen keyboard is up.
const KEYBOARD_PX = 120;

// keyboardUp: a field has focus and, where the browser can tell, the
// keyboard covers part of the screen. A composer left focused after the
// keyboard was closed does not hold back the bar for a marked passage.
function keyboardUp(): boolean {
  if (!typing(document.activeElement)) return false;
  const vv = window.visualViewport;
  return vv == null || window.innerHeight - vv.height > KEYBOARD_PX;
}

// requestFor is the capture a selection would open, or null. The passage
// is read clean from the page: only cut to the cap (code keeps brackets
// and indentation).
function requestFor(selection: Selection | null): CaptureRequest | null {
  const target = selectionTarget(selection);
  return target === null
    ? null
    : {
        messageId: target.messageId,
        excerpt: capExcerpt(target.text),
        passage: true,
      };
}

// pillPlace puts the pill above the selection's first line, below its last
// when there is no room above; null when the browser gives no rectangles.
function pillPlace(selection: Selection): { top: number; left: number } | null {
  const range = selection.getRangeAt(0);
  if (typeof range.getClientRects !== "function") return null;
  const rects = Array.from(range.getClientRects()).filter(
    (rect) => rect.width > 0 || rect.height > 0,
  );
  if (rects.length === 0) return null;
  const first = rects[0];
  const last = rects[rects.length - 1];
  const above = first.top - PILL_GAP - PILL_H;
  // Kept on screen: a selection taller than the window (dragged with
  // auto-scroll) puts it at the nearer edge.
  const top = Math.min(
    Math.max(above >= EDGE ? above : last.bottom + PILL_GAP, EDGE),
    window.innerHeight - PILL_H - EDGE,
  );
  const left = Math.min(
    Math.max(first.left, EDGE),
    window.innerWidth - PILL_W - EDGE,
  );
  return { top, left: Math.max(left, EDGE) };
}

// barBottom docks the bar just above the composer; null leaves the default.
function barBottom(): number | null {
  const composer = document.querySelector(".ui-composer");
  if (composer === null) return null;
  const top = composer.getBoundingClientRect().top;
  return top > 0 ? Math.max(window.innerHeight - top + EDGE, EDGE) : null;
}

/**
 * SelectionCapture offers a passage marked in one answer as an idea: on a
 * pointer device a pill above the selection once the mouse or key is up; on
 * a touch device a bar docked above the composer once the selection has
 * rested, clear of the native callout and handles. Neither ever changes the
 * selection or the layout. Only in a stored thread (the capture's provider).
 */
export function SelectionCapture() {
  const capture = useIdeaCapture();
  const { t } = useTranslation();
  const [shown, setShown] = useState<Shown | null>(null);
  // A press on the pill or bar: the selectionchange it may cause keeps it.
  const pressing = useRef(false);
  const touching = useRef(false);
  const settle = useRef<number | undefined>(undefined);
  // The passage Escape turned down: not offered again until the selection
  // changes (the Escape's own keyup would bring it right back).
  const dismissed = useRef<string | null>(null);
  const hide = useCallback(() => {
    window.clearTimeout(settle.current);
    setShown(null);
  }, []);
  useEscapeKey(
    () => {
      dismissed.current = shown === null ? null : keyOf(shown.request);
      hide();
    },
    { active: shown !== null },
  );

  const captureRef = useRef(capture);
  useEffect(() => {
    captureRef.current = capture;
  }, [capture]);
  const threadId = capture?.threadId ?? null;
  useEffect(() => {
    if (threadId === null) return;
    // offered is the selection's capture unless Escape turned it down.
    const offered = () => {
      const request = requestFor(document.getSelection());
      if (request === null) {
        dismissed.current = null;
        return null;
      }
      return keyOf(request) === dismissed.current ? null : request;
    };
    const showPill = (event: Event) => {
      // The pill's own click handles itself.
      if (!canHover() || ownTarget(event)) return;
      // After the browser settled the selection for this mouseup/keyup.
      window.setTimeout(() => {
        const selection = document.getSelection();
        const request = offered();
        const place =
          request === null || selection === null ? null : pillPlace(selection);
        setShown(
          request === null || place === null
            ? null
            : { kind: "pill", request, ...place },
        );
      }, 0);
    };
    const showBar = () => {
      window.clearTimeout(settle.current);
      settle.current = window.setTimeout(() => {
        // Not over the open sheet (modal on touch), not while typing.
        if (
          touching.current ||
          keyboardUp() ||
          captureRef.current?.current() != null
        )
          return;
        const request = offered();
        // Taken now, while the selection is there: a tap may collapse it
        // before its handler runs.
        setShown(
          request === null
            ? null
            : { kind: "bar", request, bottom: barBottom() },
        );
      }, SETTLE_MS);
    };
    const onSelectionChange = () => {
      // The bar's or pill's own press: it holds what it took.
      if (pressing.current) return;
      if (canHover()) {
        // Shown on mouseup/keyup only; a collapsed or moved selection
        // takes it away (and ends what Escape turned down).
        // A cheap check: this runs on every move of a drag.
        if (!selectionInAnswer(document.getSelection())) {
          dismissed.current = null;
          hide();
        }
        return;
      }
      // A handle moves: gone at once, back once it rests.
      setShown(null);
      showBar();
    };
    // A press anywhere else ends a press on the pill or bar that never
    // clicked (dragged away).
    const onPointerDown = (event: PointerEvent) => {
      if (!ownTarget(event)) pressing.current = false;
    };
    const onTouchStart = (event: TouchEvent) => {
      if (ownTarget(event)) return;
      touching.current = true;
      window.clearTimeout(settle.current);
      setShown(null);
    };
    const onTouchEnd = () => {
      touching.current = false;
      if (!canHover()) showBar();
    };
    const onFocusIn = (event: FocusEvent) => {
      if (typing(event.target as Element | null)) hide();
    };
    // A pill follows its selection on screen (a streaming answer scrolls the
    // transcript); the docked bar stays put. Once per frame.
    let frame = 0;
    const follow = () => {
      if (frame !== 0) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        setShown((current) => {
          if (current?.kind !== "pill") return current;
          const selection = document.getSelection();
          const place =
            selection !== null && selection.rangeCount > 0
              ? pillPlace(selection)
              : null;
          return place === null ? null : { ...current, ...place };
        });
      });
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("mouseup", showPill);
    document.addEventListener("keyup", showPill);
    document.addEventListener("selectionchange", onSelectionChange);
    document.addEventListener("touchstart", onTouchStart, { passive: true });
    document.addEventListener("touchend", onTouchEnd, { passive: true });
    document.addEventListener("touchcancel", onTouchEnd, { passive: true });
    document.addEventListener("focusin", onFocusIn);
    window.addEventListener("scroll", follow, true);
    window.addEventListener("resize", follow);
    return () => {
      // Another thread: nothing of the last one stays offered.
      hide();
      window.cancelAnimationFrame(frame);
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("mouseup", showPill);
      document.removeEventListener("keyup", showPill);
      document.removeEventListener("selectionchange", onSelectionChange);
      document.removeEventListener("touchstart", onTouchStart);
      document.removeEventListener("touchend", onTouchEnd);
      document.removeEventListener("touchcancel", onTouchEnd);
      document.removeEventListener("focusin", onFocusIn);
      window.removeEventListener("scroll", follow, true);
      window.removeEventListener("resize", follow);
    };
  }, [threadId, hide]);

  if (capture === null || shown === null) return null;
  const open = () => {
    pressing.current = false;
    // A capture being saved stays until its answer; the offer stays too,
    // for a tap once it is saved.
    if (captureSaving()) return;
    // hide also drops a settle the tap's own touchend armed: the bar must
    // not come back over the open sheet.
    hide();
    capture.open(shown.request);
  };
  // Never takes the selection or the focus: the press is swallowed, the
  // click opens the capture with the passage taken when it showed. The
  // press holds until that click (on touch it comes a task later) or a
  // press elsewhere.
  const keep = {
    onPointerDown: (event: ReactPointerEvent) => {
      event.preventDefault();
      pressing.current = true;
    },
    onPointerCancel: () => {
      pressing.current = false;
    },
    onMouseDown: (event: ReactMouseEvent) => event.preventDefault(),
    onClick: open,
    // The open capture's outside press ignores its own answer's offer.
    "data-idea-bulb": shown.request.messageId,
    "data-idea-select": "",
    type: "button" as const,
  };
  const label = t("ideas.capture.button");
  return createPortal(
    shown.kind === "pill" ? (
      <button
        {...keep}
        className="ui-idea-select-in fixed z-40 inline-flex h-[30px] items-center gap-1.5 whitespace-nowrap rounded-[10px] border border-[#454540] bg-[#363632] pr-2.5 pl-2 font-sans text-[14px] text-ink-bright shadow-[0_18px_32px_rgba(0,0,0,0.38)] hover:bg-[#3f3f3a]"
        style={{ top: shown.top, left: shown.left }}
      >
        <Icon name="bulb" size="17px" className="text-unread-ink" />
        {label}
      </button>
    ) : (
      <div
        className="pointer-events-none fixed inset-x-0 z-40 flex justify-center px-4"
        style={{
          bottom:
            shown.bottom === null
              ? "calc(16px + env(safe-area-inset-bottom))"
              : shown.bottom,
        }}
      >
        <button
          {...keep}
          className="ui-idea-select-in pointer-events-auto inline-flex h-11 items-center gap-2 rounded-xl border border-[#454540] bg-[#363632] px-4 font-sans text-[15px] text-ink-bright shadow-[0_18px_32px_rgba(0,0,0,0.45)]"
        >
          <Icon name="bulb" size="18px" className="text-unread-ink" />
          {label}
        </button>
      </div>
    ),
    document.body,
  );
}
