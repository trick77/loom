import { useState } from "react";

import type { Citation } from "../api";

type CombinedSource = {
  filename: string;
  references: number;
  bestSnippet: string;
  bestScore: number;
};

// combineLikeSources groups per-chunk citations by document (filename), mirroring
// AnythingLLM: one chip per document with a reference count, keeping the
// highest-scoring snippet for the detail view.
export function combineLikeSources(sources: Citation[]): CombinedSource[] {
  const byFile = new Map<string, CombinedSource>();
  for (const source of sources) {
    const existing = byFile.get(source.filename);
    if (existing) {
      existing.references += 1;
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
      });
    }
  }
  return [...byFile.values()].sort((a, b) => b.bestScore - a.bestScore);
}

// MessageCitations renders the document sources that informed an assistant answer
// as a "Sources" row of chips; clicking one reveals its matched snippet.
export function MessageCitations({ citations }: { citations?: Citation[] }) {
  const [openFile, setOpenFile] = useState<string | null>(null);
  if (citations === undefined || citations.length === 0) return null;
  const combined = combineLikeSources(citations);
  const open = combined.find((source) => source.filename === openFile) ?? null;

  return (
    <div className="ui-meta-text mt-1 space-y-2 text-[#9a8f7e]">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[#858178]">Sources</span>
        {combined.map((source) => (
          <button
            key={source.filename}
            type="button"
            className="inline-flex items-center gap-1 rounded-ui border border-[#4b4a46] bg-[#2a2a28] px-2 py-0.5 text-[#d8d4ca] transition-colors hover:bg-[#343432]"
            onClick={() => setOpenFile(openFile === source.filename ? null : source.filename)}
            title={`${source.filename} (${source.references} match${source.references > 1 ? "es" : ""})`}
          >
            <span className="max-w-[180px] truncate">{source.filename}</span>
            {source.references > 1 && <span className="text-[#858178]">×{source.references}</span>}
          </button>
        ))}
      </div>
      {open !== null && (
        <div className="rounded-ui border border-[#4b4a46] bg-[#222220] px-3 py-2 text-[#c8c4ba]">
          <div className="mb-1 flex items-center justify-between">
            <span className="truncate text-[#e8e4da]">{open.filename}</span>
            <span className="text-[#858178]">relevance {(open.bestScore * 100).toFixed(0)}%</span>
          </div>
          <p className="whitespace-pre-wrap">{open.bestSnippet}</p>
        </div>
      )}
    </div>
  );
}
