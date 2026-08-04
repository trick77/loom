import { useEffect, useRef, type RefObject } from "react";
import { useTranslation } from "react-i18next";

import type { Citation } from "../api";
import type { DisplayMap } from "./sourcePills";
import { Icon } from "./Icon";
import { hostOf, SourceFavicon } from "./SourceFavicon";

// How many sources render above the "More" divider.
const PRIMARY_COUNT = 4;

// SourcesSidebar is the right-side drawer listing the web sources that informed an
// answer, each with its favicon, site, title and the snippet it delivered. It is a
// full-screen sheet on mobile and a fixed drawer on desktop (mirrors Sidebar.tsx),
// closes on the X / backdrop / Escape, and mounts only while open so favicons load
// on demand.
export function SourcesSidebar({
  open,
  sources,
  display,
  selected,
  selectionNonce,
  onHoverSource,
  onClose,
}: {
  open: boolean;
  sources: Citation[];
  /** Persisted index -> the number shown in the prose. Absent = not cited. */
  display?: DisplayMap;
  /** Display numbers the reader pinned by clicking a marker in the answer. */
  selected?: ReadonlySet<number>;
  /**
   * Bumped on every marker click. The selection alone cannot drive the scroll:
   * clicking the same marker twice leaves it unchanged, and the second click would
   * silently do nothing after the reader had scrolled away.
   */
  selectionNonce?: number;
  /** Reports the row under the cursor, so its markers light up in the prose. */
  onHoverSource?(number?: number): void;
  onClose(): void;
}) {
  const { t } = useTranslation();
  const firstSelected = useRef<HTMLAnchorElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  // A selected source can sit below the "More" divider and start off-screen, so the
  // click has to bring it into view. "nearest" keeps a source that is already visible
  // exactly where it is rather than yanking the list to centre it.
  useEffect(() => {
    if (!open) return;
    firstSelected.current?.scrollIntoView({ block: "nearest" });
  }, [open, selectionNonce]);

  if (!open || sources.length === 0) return null;
  const primary = sources.slice(0, PRIMARY_COUNT);
  const rest = sources.slice(PRIMARY_COUNT);

  return (
    <>
      <div
        className="ui-drawer-backdrop fixed inset-0 z-40 bg-black/50"
        onClick={onClose}
        aria-hidden="true"
      />
      <aside
        role="dialog"
        aria-modal="true"
        aria-label={t("citations.sources")}
        className="ui-drawer fixed inset-y-0 right-0 z-50 flex w-full max-w-full flex-col border-l border-[#343432] bg-[#1c1b18] md:w-[400px] md:max-w-[92vw]"
      >
        <div className="flex items-center justify-between border-b border-[#343432] px-4 py-3">
          <h3 className="font-serif text-[1.15rem] font-medium text-[#f3f0e8]">
            {t("citations.sources")}
          </h3>
          <button
            type="button"
            onClick={onClose}
            aria-label={t("citations.close")}
            className="grid h-8 w-8 place-items-center rounded-md text-[#aaa79e] transition-colors hover:bg-[#2a2a28] hover:text-[#f3f0e8]"
          >
            <Icon name="close" size="18px" />
          </button>
        </div>
        <div className="ui-sidebar-scroll min-h-0 flex-1 overflow-y-auto px-2 py-2">
          {primary.map((source, index) => (
            <SourceCard
              key={cardKey(source, index)}
              citation={source}
              number={display?.get(source.index ?? 0)}
              selected={selected}
              scrollRef={
                isScrollTarget(sources, display, selected, source)
                  ? firstSelected
                  : undefined
              }
              onHoverSource={onHoverSource}
            />
          ))}
          {rest.length > 0 && (
            <div className="my-1.5 flex items-center gap-2 px-3 text-[0.8rem] font-semibold text-[#858178]">
              <span>{t("citations.more")}</span>
              <span className="h-px flex-1 bg-[#2b2a27]" />
            </div>
          )}
          {rest.map((source, index) => (
            <SourceCard
              key={cardKey(source, PRIMARY_COUNT + index)}
              citation={source}
              number={display?.get(source.index ?? 0)}
              selected={selected}
              scrollRef={
                isScrollTarget(sources, display, selected, source)
                  ? firstSelected
                  : undefined
              }
              onHoverSource={onHoverSource}
            />
          ))}
        </div>
      </aside>
    </>
  );
}

function cardKey(citation: Citation, index: number): string {
  return `${citation.index ?? index}-${citation.url ?? ""}`;
}

// isScrollTarget reports whether `citation` is the topmost selected source, the one
// the list scrolls to. A run of markers selects several sources at once and they can
// straddle the "More" divider; scrolling to the first keeps the reading order of the
// list, and the others follow it downward.
function isScrollTarget(
  sources: Citation[],
  display: DisplayMap | undefined,
  selected: ReadonlySet<number> | undefined,
  citation: Citation,
): boolean {
  if (selected === undefined || selected.size === 0) return false;
  const first = sources.find((source) => {
    const number = display?.get(source.index ?? 0);
    return number !== undefined && selected.has(number);
  });
  return first === citation;
}

// number is the reader-facing citation number, taken from the display map rather
// than the row's position: while an answer streams the list also carries sources
// that have not been cited yet, and those have no number at all. Using the position
// would hand them one, pointing at a marker that does not exist in the prose.
function SourceCard({
  citation,
  number,
  selected,
  scrollRef,
  onHoverSource,
}: {
  citation: Citation;
  number?: number;
  selected?: ReadonlySet<number>;
  scrollRef?: RefObject<HTMLAnchorElement | null>;
  onHoverSource?(number?: number): void;
}) {
  const host = hostOf(citation.url);
  const site = citation.filename !== "" ? citation.filename : host;
  const title =
    citation.title !== undefined && citation.title.trim() !== ""
      ? citation.title
      : (citation.url ?? "");
  // Pinned and hovered are deliberately different looks. Sharing one would make the
  // cursor destroy the selection it passes over — the reason a hover-clears-selection
  // rule looks necessary at all — so hover stays the transient wash it already was,
  // and the selection gets its own accent rail, which the two states then stack.
  const isSelected = number !== undefined && selected?.has(number) === true;
  return (
    <a
      ref={scrollRef}
      href={citation.url}
      target="_blank"
      rel="noreferrer noopener"
      onMouseEnter={() => onHoverSource?.(number)}
      onMouseLeave={() => onHoverSource?.(undefined)}
      onFocus={() => onHoverSource?.(number)}
      onBlur={() => onHoverSource?.(undefined)}
      aria-current={isSelected ? "true" : undefined}
      className={`ui-source-card block rounded-[10px] px-3 py-2.5 no-underline transition-colors ${
        isSelected ? "ui-source-card-selected" : ""
      }`}
    >
      <div className="mb-1 flex items-center gap-2 text-[13px] text-[#8a887f]">
        {/* Same plate, family, weight and tabular figures as the inline marker it
            pairs with — but not its superscript rise: here the number is a row
            element, not a marker riding a text baseline. */}
        {number !== undefined && (
          <span className="ui-source-number">{number}</span>
        )}
        <span className="truncate">{site}</span>
      </div>
      <p className="mb-1 flex items-center gap-2 text-[15px] leading-snug font-semibold text-[#f3f0e8]">
        <span>{title}</span>
        <SourceFavicon citation={citation} size={16} />
      </p>
      {citation.snippet !== undefined && citation.snippet !== "" && (
        <p
          className="text-[13px] leading-5 text-[#8a887f]"
          style={{
            display: "-webkit-box",
            WebkitLineClamp: 2,
            WebkitBoxOrient: "vertical",
            overflow: "hidden",
          }}
        >
          {citation.snippet}
        </p>
      )}
    </a>
  );
}
