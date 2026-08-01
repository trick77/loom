import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { Citation } from "../api";
import type { DisplayMap } from "./sourcePills";
import { hostOf, SourceFavicon } from "./SourceFavicon";
import { SourcesSidebar } from "./SourcesSidebar";

type CombinedSource = {
  filename: string;
  references: number;
  bestSnippet: string;
  bestScore: number;
  full: boolean;
  // The [n] marker the answer cites this document by. Chunks of one document all
  // share it, so the first is representative. Undefined for older messages, whose
  // document citations were stored before documents were numbered.
  index?: number;
};

// combineLikeSources groups per-chunk citations by document (filename), mirroring
// AnythingLLM: one chip per document with a reference count, keeping the
// highest-scoring snippet for the detail view. A document injected in full is
// flagged so the chip reads "full document" rather than an excerpt count.
export function combineLikeSources(sources: Citation[]): CombinedSource[] {
  const byFile = new Map<string, CombinedSource>();
  for (const source of sources) {
    const existing = byFile.get(source.filename);
    if (existing) {
      existing.references += 1;
      existing.full = existing.full || source.full === true;
      if (source.score > existing.bestScore) {
        existing.bestScore = source.score;
        existing.bestSnippet = source.snippet;
      }
    } else {
      byFile.set(source.filename, {
        filename: source.filename,
        references: 1,
        bestSnippet: source.snippet,
        bestScore: source.score,
        full: source.full === true,
        index: source.index,
      });
    }
  }
  return [...byFile.values()].sort((a, b) => b.bestScore - a.bestScore);
}

// isWebSource reports whether a citation is a web-search source (carries a URL)
// rather than a RAG document chunk.
function isWebSource(citation: Citation): boolean {
  return typeof citation.url === "string" && citation.url !== "";
}

// combineWebSources deduplicates web citations by URL, so the "Sources" row lists
// each distinct page once. filename holds the site-name label.
//
// Input order is preserved deliberately: callers pass the list already ordered by
// first citation (assignDisplayNumbers), and the sidebar derives each card's number
// from its position. Re-sorting by the persisted citation.index here would restore
// Tavily arrival order and desync the cards from the inline [n] markers.
function combineWebSources(sources: Citation[]): Citation[] {
  const seen = new Set<string>();
  const unique: Citation[] = [];
  for (const source of sources) {
    if (source.url === undefined || seen.has(source.url)) continue;
    seen.add(source.url);
    unique.push(source);
  }
  return unique;
}

// dedupeByDomain keeps the first web source per registrable domain so the favicon
// pile shows one icon per distinct site — pages from different subdomains of the
// same site (github.com, docs.github.com, gist.github.com) share a favicon and
// would otherwise render as visual duplicates. Keying on hostname alone doesn't
// collapse those, so we dedupe on the backend-computed site label (`filename`,
// the registrable domain's main label via the public-suffix list, e.g.
// "Github") — the correct grouping level. Sources whose label is empty (an IP or
// bare public-suffix host the backend couldn't label) fall back to their hostname
// and then their (already URL-unique) url, so they are neither merged together
// nor dropped. Input order (citation order) is kept.
export function dedupeByDomain(sources: Citation[]): Citation[] {
  const seen = new Set<string>();
  const unique: Citation[] = [];
  for (const source of sources) {
    const key =
      source.filename.trim().toLowerCase() || hostOf(source.url) || source.url;
    if (key === undefined || key === "" || seen.has(key)) continue;
    seen.add(key);
    unique.push(source);
  }
  return unique;
}

// MessageCitations renders the sources that informed an assistant answer as a
// "Sources" row of chips. Document sources reveal their matched snippet on click;
// web sources link out to the page.
export function MessageCitations({
  citations,
  display,
}: {
  citations?: Citation[];
  /** Persisted index -> the number shown in the prose. Absent = not cited. */
  display?: DisplayMap;
}) {
  const { t } = useTranslation();
  const [openFile, setOpenFile] = useState<string | null>(null);
  const [sourcesOpen, setSourcesOpen] = useState(false);
  if (citations === undefined || citations.length === 0) return null;
  const combined = combineLikeSources(citations.filter((c) => !isWebSource(c)));
  const webSources = combineWebSources(citations.filter(isWebSource));
  // The pile is a per-site visual summary, so collapse to one favicon per site;
  // the sidebar still lists every distinct page from `webSources`.
  const faviconSources = dedupeByDomain(webSources);
  if (combined.length === 0 && webSources.length === 0) return null;
  const open = combined.find((source) => source.filename === openFile) ?? null;

  return (
    <div className="ui-meta-text mt-1 space-y-2 text-[#9a8f7e]">
      <div className="flex flex-wrap items-center gap-2">
        {webSources.length > 0 ? (
          <button
            type="button"
            onClick={() => setSourcesOpen(true)}
            className="group inline-flex items-center gap-2 rounded-full py-0.5 pr-1 transition-colors"
            aria-haspopup="dialog"
            aria-expanded={sourcesOpen}
          >
            {/* pl-[3px] offsets the leftmost favicon's 3px ring so the icons' visible
                edge lines up with the prose and action row above. `isolate` keeps the
                favicons' per-icon z-index (used only to overlap within the pile) from
                leaking into the transcript's stacking context and painting over the
                sticky composer while scrolling. */}
            <span className="isolate flex items-center pl-[3px]">
              {faviconSources.slice(0, 10).map((source, index, shown) => (
                <SourceFavicon
                  key={`fav-${source.index}-${source.url}`}
                  citation={source}
                  size={18}
                  className={`relative ring-[3px] ring-[#1a1917] ${index > 0 ? "-ml-[3px]" : ""}`}
                  style={{ zIndex: shown.length - index }}
                />
              ))}
            </span>
            <span className="text-[#858178] transition-colors group-hover:text-[#d8d4ca]">
              {t("citations.sources")}
            </span>
          </button>
        ) : (
          <span className="text-[#858178]">{t("citations.sources")}</span>
        )}
        {combined.map((source) => (
          <button
            key={source.filename}
            type="button"
            className="inline-flex items-center gap-1 rounded-ui border border-[#4b4a46] bg-[#2a2a28] px-2 py-0.5 text-[#d8d4ca] transition-colors hover:bg-[#343432]"
            onClick={() =>
              setOpenFile(openFile === source.filename ? null : source.filename)
            }
            title={
              source.full
                ? t("citations.tooltipFull", { filename: source.filename })
                : t("citations.tooltipMatches", {
                    filename: source.filename,
                    count: source.references,
                  })
            }
          >
            {display?.get(source.index ?? 0) !== undefined && (
              <span className="tabular-nums text-[#8a887f]">
                {display.get(source.index ?? 0)}
              </span>
            )}
            <span className="max-w-[180px] truncate">{source.filename}</span>
            {source.full ? (
              <span className="text-[#858178]">
                {t("citations.fullDocument")}
              </span>
            ) : (
              source.references > 1 && (
                <span className="text-[#858178]">
                  {t("citations.excerpts", { count: source.references })}
                </span>
              )
            )}
          </button>
        ))}
      </div>
      {open !== null && (
        <div className="rounded-ui border border-[#4b4a46] bg-[#222220] px-3 py-2 text-[#c8c4ba]">
          <div className="mb-1 flex items-center justify-between">
            <span className="truncate text-[#e8e4da]">{open.filename}</span>
            <span className="text-[#858178]">
              {open.full
                ? t("citations.fullDocument")
                : t("citations.relevance", {
                    percent: (open.bestScore * 100).toFixed(0),
                  })}
            </span>
          </div>
          <p className="whitespace-pre-wrap">{open.bestSnippet}</p>
        </div>
      )}
      <SourcesSidebar
        open={sourcesOpen}
        sources={webSources}
        display={display}
        onClose={() => setSourcesOpen(false)}
      />
    </div>
  );
}
