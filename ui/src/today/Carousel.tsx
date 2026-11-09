import {
  type FocusEvent,
  type PointerEvent,
  type ReactNode,
  useEffect,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";

import { useMediaQuery } from "../chat/useMediaQuery";

export type Slide = { key: string; name: string; content: ReactNode };

// SWIPE_PX is how far a finger or mouse must travel sideways to turn a card.
export const SWIPE_PX = 40;

// Carousel shows one slide at a time, turned by the arrows, the dots, the
// arrow keys or a swipe, and with autoAdvanceMs by itself, back to the first
// after the last. Every slide stays rendered side by side, so the tallest
// sets the height and the grid around it never jumps; the ones out of view
// are inert. It always opens on the first: a warning lives there.
export function Carousel({
  label,
  slides,
  autoAdvanceMs,
}: {
  label: string;
  slides: Slide[];
  autoAdvanceMs?: number;
}) {
  const { t } = useTranslation();
  const [index, setIndex] = useState(0);
  const start = useRef<{ x: number; y: number } | null>(null);
  const count = slides.length;
  const current = Math.min(index, count - 1);
  const go = (to: number) => setIndex(Math.max(0, Math.min(count - 1, to)));

  // It holds still while read: under the mouse, with keyboard focus inside
  // (a keyboard or screen reader user), on a hidden page, and never turns for
  // reduced motion.
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [hidden, setHidden] = useState(() => document.hidden);
  const reduced = useMediaQuery("(prefers-reduced-motion: reduce)");
  useEffect(() => {
    const onChange = () => setHidden(document.hidden);
    document.addEventListener("visibilitychange", onChange);
    return () => document.removeEventListener("visibilitychange", onChange);
  }, []);
  const auto = autoAdvanceMs !== undefined && count > 1 && !reduced;
  const running = auto && !hovered && !focused && !hidden;
  // Every turn, by hand or by itself, starts the time over.
  useEffect(() => {
    if (!running) return;
    const timer = setTimeout(
      () => setIndex((current + 1) % count),
      autoAdvanceMs,
    );
    return () => clearTimeout(timer);
  }, [running, current, count, autoAdvanceMs]);
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
  };
  const onPointerUp = (event: PointerEvent) => {
    const from = start.current;
    start.current = null;
    if (from === null) return;
    const dx = event.clientX - from.x;
    const dy = event.clientY - from.y;
    if (Math.abs(dx) < SWIPE_PX || Math.abs(dx) < Math.abs(dy)) return;
    go(current + (dx < 0 ? 1 : -1));
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
        className="flex-1 touch-pan-y overflow-hidden select-none"
        data-testid="carousel-track"
        onPointerCancel={() => (start.current = null)}
        onPointerDown={onPointerDown}
        onPointerUp={onPointerUp}
      >
        <div
          className="flex h-full transition-transform duration-300 ease-out motion-reduce:transition-none"
          style={{ transform: `translateX(-${current * 100}%)` }}
        >
          {slides.map((slide, i) => (
            <div
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
              speak up every 15 s. */}
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
                      key={`${current}/${running}`}
                      style={
                        running
                          ? {
                              animation: `card-progress ${autoAdvanceMs}ms linear forwards`,
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
