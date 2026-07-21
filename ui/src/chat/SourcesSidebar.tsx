import { useEffect } from "react";
import { useTranslation } from "react-i18next";

import type { Citation } from "../api";
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
  onClose,
}: {
  open: boolean;
  sources: Citation[];
  onClose(): void;
}) {
  const { t } = useTranslation();

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

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
            <SourceCard key={cardKey(source, index)} citation={source} />
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

function SourceCard({ citation }: { citation: Citation }) {
  const host = hostOf(citation.url);
  const site = citation.filename !== "" ? citation.filename : host;
  const title =
    citation.title !== undefined && citation.title.trim() !== ""
      ? citation.title
      : (citation.url ?? "");
  return (
    <a
      href={citation.url}
      target="_blank"
      rel="noreferrer noopener"
      className="block rounded-[10px] px-3 py-2.5 no-underline transition-colors hover:bg-[#201f1c]"
    >
      <div className="mb-1 flex items-center gap-2 text-[13px] text-[#8a887f]">
        <SourceFavicon citation={citation} size={16} />
        <span className="truncate">{site}</span>
      </div>
      <p className="mb-1 text-[15px] font-semibold leading-snug text-[#f3f0e8]">
        {title}
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
