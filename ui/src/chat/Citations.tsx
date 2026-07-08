import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { Citation } from "../api";
import { SourceFavicon } from "./SourceFavicon";
import { SourcesSidebar } from "./SourcesSidebar";

type CombinedSource = {
  filename: string;
  references: number;
  bestSnippet: string;
  bestScore: number;
  full: boolean;
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

// combineWebSources deduplicates web citations by URL and orders them by their
// [n] index, so the "Sources" row lists each distinct page once in citation
// order. filename holds the site-name label.
function combineWebSources(sources: Citation[]): Citation[] {
  const seen = new Set<string>();
  const unique: Citation[] = [];
  for (const source of sources) {
    if (source.url === undefined || seen.has(source.url)) continue;
    seen.add(source.url);
    unique.push(source);
  }
  return unique.sort((a, b) => (a.index ?? 0) - (b.index ?? 0));
}

// MessageCitations renders the sources that informed an assistant answer as a
// "Sources" row of chips. Document sources reveal their matched snippet on click;
// web sources link out to the page.
export function MessageCitations({ citations }: { citations?: Citation[] }) {
  const { t } = useTranslation();
  const [openFile, setOpenFile] = useState<string | null>(null);
  const [sourcesOpen, setSourcesOpen] = useState(false);
  if (citations === undefined || citations.length === 0) return null;
  const combined = combineLikeSources(citations.filter((c) => !isWebSource(c)));
  const webSources = combineWebSources(citations.filter(isWebSource));
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
            {/* pl-0.5 offsets the leftmost favicon's 2px ring so the icons' visible
                edge lines up with the prose and action row above. */}
            <span className="flex items-center pl-0.5">
              {webSources.slice(0, 10).map((source, index) => (
                <SourceFavicon
                  key={`fav-${source.index}-${source.url}`}
                  citation={source}
                  size={18}
                  className={`ring-2 ring-[#1a1917] ${index > 0 ? "-ml-2.5" : ""}`}
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
            onClick={() => setOpenFile(openFile === source.filename ? null : source.filename)}
            title={
              source.full
                ? t("citations.tooltipFull", { filename: source.filename })
                : t("citations.tooltipMatches", { filename: source.filename, count: source.references })
            }
          >
            <span className="max-w-[180px] truncate">{source.filename}</span>
            {source.full ? (
              <span className="text-[#858178]">{t("citations.fullDocument")}</span>
            ) : (
              source.references > 1 && (
                <span className="text-[#858178]">{t("citations.excerpts", { count: source.references })}</span>
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
                : t("citations.relevance", { percent: (open.bestScore * 100).toFixed(0) })}
            </span>
          </div>
          <p className="whitespace-pre-wrap">{open.bestSnippet}</p>
        </div>
      )}
      <SourcesSidebar open={sourcesOpen} sources={webSources} onClose={() => setSourcesOpen(false)} />
    </div>
  );
}
