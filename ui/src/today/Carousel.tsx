import { type ReactNode, type PointerEvent, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

export type Slide = { key: string; name: string; content: ReactNode };

// SWIPE_PX is how far a finger or mouse must travel sideways to turn a card.
export const SWIPE_PX = 40;

// Carousel shows one slide at a time, turned by the arrows, the dots, the
// arrow keys or a swipe. Every slide stays rendered side by side, so the
// tallest sets the height and the grid around it never jumps; the ones out
// of view are inert. It always opens on the first: a warning lives there.
export function Carousel({
  label,
  slides,
}: {
  label: string;
  slides: Slide[];
}) {
  const { t } = useTranslation();
  const [index, setIndex] = useState(0);
  const start = useRef<{ x: number; y: number } | null>(null);
  const count = slides.length;
  const current = Math.min(index, count - 1);
  const go = (to: number) => setIndex(Math.max(0, Math.min(count - 1, to)));

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
          <span aria-live="polite" data-testid="card-name">
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
                <span
                  className={`size-1.5 rounded-full transition-colors ${
                    i === current
                      ? "bg-ink-soft"
                      : "bg-border-strong group-hover:bg-muted"
                  }`}
                />
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
