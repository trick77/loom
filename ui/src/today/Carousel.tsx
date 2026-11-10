import {
  type FocusEvent,
  type PointerEvent,
  type ReactNode,
  type TransitionEvent,
  useEffect,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";

import { STEP_MS } from "./useTileClock";

export type Slide = { key: string; name: string; content: ReactNode };

// SWIPE_PX is how far a finger or mouse must travel sideways to turn a card.
export const SWIPE_PX = 40;

// Clock is what the beat (useTileClock) tells a tile: whether it turns by
// itself at all, whether it does right now, whether it is due next, and the
// count of turns by hand, each of which starts its card's time over.
export type Clock = {
  auto: boolean;
  running: boolean;
  due: boolean;
  epoch: number;
};

// Carousel shows one slide at a time, turned by a click on it, the arrows,
// the dots, the arrow keys or a swipe; the beat turns it by itself. After the
// last a click turns on to the first. Every slide stays rendered
// side by side, so the tallest sets the height and the grid around it never
// jumps; the ones out of view are inert.
export function Carousel({
  label,
  slides,
  index,
  onTurn,
  onHold,
  clock,
}: {
  label: string;
  slides: Slide[];
  index: number;
  onTurn(to: number): void;
  onHold(held: boolean): void;
  clock: Clock;
}) {
  const { t } = useTranslation();
  const start = useRef<{ x: number; y: number } | null>(null);
  // swiped skips the click that ends a swipe: the swipe has turned already.
  const swiped = useRef(false);
  const count = slides.length;
  const current = Math.min(index, count - 1);
  const auto = clock.auto && count > 1;
  const running = auto && clock.running;

  // The wrap from the last to the first slides on, not back past every card:
  // the first is carried past the last and slides in from the right; once it
  // stands, the row settles back at the start unseen. A step by the arrows,
  // keys, dots or a swipe slides back when it lands on the first.
  const [shown, setShown] = useState({
    index: current,
    wrap: false,
    jump: false,
  });
  const [stepping, setStepping] = useState(false);
  if (shown.index !== current) {
    setShown({
      index: current,
      wrap: !stepping && shown.index === count - 1 && current === 0,
      // A turn before the wrap stands would slide back past every card.
      jump: shown.wrap,
    });
    if (stepping) setStepping(false);
  }
  // A card's own transitions bubble up here: only the row's settles it.
  const settle = (event: TransitionEvent) => {
    if (event.target !== event.currentTarget || !shown.wrap) return;
    setShown({ index: current, wrap: false, jump: true });
  };
  // A step that moves nothing (past an end, the shown card's dot) is no
  // turn: it would start the beat over.
  const go = (to: number) => {
    const target = Math.max(0, Math.min(count - 1, to));
    if (target === current) return;
    setStepping(true);
    onTurn(target);
  };

  // The bar fills over the card's two beats; the tile due next is one beat
  // in. Fixed when the card's time starts, not on every beat.
  const barKey = `${current}/${running}/${clock.epoch}`;
  const [bar, setBar] = useState({ key: barKey, due: clock.due });
  if (bar.key !== barKey) setBar({ key: barKey, due: clock.due });

  // It holds still while read: under the mouse, or with keyboard focus inside
  // (a keyboard or screen reader user).
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  useEffect(() => {
    onHold(hovered || focused);
  }, [hovered, focused, onHold]);
  useEffect(() => () => onHold(false), [onHold]);
  const onBlur = (event: FocusEvent) => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
      setFocused(false);
    }
  };
  // Focus holds the card for keyboard and screen reader users only: a click
  // or tap leaves its button focused until the next click elsewhere, which
  // held the cards for good. A key pressed anywhere makes focus count again.
  const pointing = useRef(false);
  useEffect(() => {
    const onKey = () => (pointing.current = false);
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, []);
  // Only a mouse hovers: a tap's emulated mouseenter has no leave.
  const onHover = (event: PointerEvent, over: boolean) => {
    if (event.pointerType === "mouse") setHovered(over);
  };

  const onPointerDown = (event: PointerEvent) => {
    start.current = { x: event.clientX, y: event.clientY };
    swiped.current = false;
  };
  const onPointerUp = (event: PointerEvent) => {
    const from = start.current;
    start.current = null;
    if (from === null) return;
    const dx = event.clientX - from.x;
    const dy = event.clientY - from.y;
    if (Math.abs(dx) < SWIPE_PX || Math.abs(dx) < Math.abs(dy)) return;
    swiped.current = true;
    go(current + (dx < 0 ? 1 : -1));
  };
  const onClick = () => {
    if (swiped.current) {
      swiped.current = false;
      return;
    }
    if (count > 1) onTurn((current + 1) % count);
  };

  if (count === 0) return null;
  return (
    <section
      aria-label={label}
      aria-roledescription={t("today.cards.carousel")}
      className="flex h-full flex-col gap-3"
      onBlur={onBlur}
      onFocus={() => {
        if (!pointing.current) setFocused(true);
      }}
      onPointerDownCapture={() => (pointing.current = true)}
      onPointerEnter={(event) => onHover(event, true)}
      onPointerLeave={(event) => onHover(event, false)}
      onKeyDown={(event) => {
        if (event.key === "ArrowRight") go(current + 1);
        else if (event.key === "ArrowLeft") go(current - 1);
        else return;
        event.preventDefault();
      }}
    >
      <div
        className={`flex-1 touch-pan-y overflow-hidden select-none ${count > 1 ? "cursor-pointer" : ""}`}
        data-testid="carousel-track"
        onClick={onClick}
        onPointerCancel={() => (start.current = null)}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
      >
        <div
          className={`flex h-full ${shown.jump ? "" : "transition-transform duration-300 ease-out motion-reduce:transition-none"}`}
          onTransitionEnd={settle}
          style={{
            transform: `translateX(-${(shown.wrap ? count : current) * 100}%)`,
          }}
        >
          {slides.map((slide, i) => (
            <div
              style={
                shown.wrap && i === 0
                  ? { transform: `translateX(${count * 100}%)` }
                  : undefined
              }
              aria-hidden={i === current ? undefined : true}
              aria-label={t("today.cards.position", {
                name: slide.name,
                n: i + 1,
                count,
              })}
              aria-roledescription={t("today.cards.slide")}
              className="w-full shrink-0"
              inert={i !== current}
              key={slide.key}
              role="group"
            >
              {slide.content}
            </div>
          ))}
        </div>
      </div>
      {count > 1 && (
        <div className="flex items-center justify-between gap-2 text-[12px] text-muted">
          {/* Announced on a turn by hand only: turning by itself, it would
              speak up on every beat. */}
          <span aria-live={running ? "off" : "polite"} data-testid="card-name">
            {slides[current].name}
          </span>
          <div className="flex items-center">
            <ArrowButton
              direction="previous"
              disabled={current === 0}
              label={t("today.cards.previous")}
              onClick={() => go(current - 1)}
            />
            {slides.map((slide, i) => (
              <button
                aria-current={i === current ? "true" : undefined}
                aria-label={slide.name}
                className="group grid size-5 place-items-center"
                key={slide.key}
                onClick={() => go(i)}
                type="button"
              >
                {i === current && auto ? (
                  // The current card's dot fills up until it turns; held
                  // still, it stands full.
                  <span
                    className="flex h-1.5 w-5 overflow-hidden rounded-full bg-border-strong"
                    data-testid="card-progress"
                  >
                    <span
                      className="w-full origin-left rounded-full bg-ink-soft"
                      key={barKey}
                      style={
                        running
                          ? {
                              animation: `card-progress ${2 * STEP_MS}ms linear ${bar.due ? -STEP_MS : 0}ms forwards`,
                            }
                          : undefined
                      }
                    />
                  </span>
                ) : (
                  <span
                    className={`size-1.5 rounded-full transition-colors ${
                      i === current
                        ? "bg-ink-soft"
                        : "bg-border-strong group-hover:bg-muted"
                    }`}
                  />
                )}
              </button>
            ))}
            <ArrowButton
              direction="next"
              disabled={current === count - 1}
              label={t("today.cards.next")}
              onClick={() => go(current + 1)}
            />
          </div>
        </div>
      )}
    </section>
  );
}

function ArrowButton({
  direction,
  disabled,
  label,
  onClick,
}: {
  direction: "previous" | "next";
  disabled: boolean;
  label: string;
  onClick(): void;
}) {
  return (
    <button
      aria-label={label}
      className="grid size-6 place-items-center rounded-md text-muted hover:bg-raised hover:text-ink disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-muted"
      disabled={disabled}
      onClick={onClick}
      type="button"
    >
      <svg
        aria-hidden="true"
        fill="none"
        height="14"
        stroke="currentColor"
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth="2"
        viewBox="0 0 24 24"
        width="14"
      >
        <path d={direction === "next" ? "M9 6l6 6-6 6" : "M15 6l-6 6 6 6"} />
      </svg>
    </button>
  );
}
