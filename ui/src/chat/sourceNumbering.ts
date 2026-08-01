import type { Citation } from "../api";

// The backend stamps every gathered URL with a stable [n] at fetch time, in Tavily
// arrival order, and that is the number the model sees and emits. It is a poor
// *display* number: the model typically cites 4 of 10 gathered sources, so an
// answer can open with [3] while sources it never used sit at the top of the list.
//
// assignDisplayNumbers derives a presentation numbering from the answer text:
// 1, 2, 3… in the order the model first cites each source. The persisted
// citation.index is never mutated — it is the model's contract, the number stamped
// into the tool output it reads — so nothing in the DB or backend changes and
// historical messages renumber correctly with no migration.
//
// The mapping is append-only: a display number, once assigned, never changes as
// more text arrives. That is what makes it safe to run against a partial answer
// mid-stream — pills and sidebar rows never reshuffle, they only get appended to.

export type DisplayNumbering = {
  // persisted citation.index -> display number
  display: Map<number, number>;
  // the citations to show, in display order
  ordered: Citation[];
};

const MARKER = /\[(\d+)\]/g;
// A fenced block, then an inline span. Markers inside code are array indices
// (arr[0], argv[2]), not citations — mirrors the code/pre skipping in
// rehypeSourcePills, which never turns those into pills either.
const FENCED = /```[\s\S]*?(?:```|$)/g;
const INLINE_CODE = /`[^`\n]*`/g;

// stripCode blanks out code regions while preserving the surrounding text, so
// marker order and position are unaffected by the removal.
function stripCode(content: string): string {
  return content.replace(FENCED, " ").replace(INLINE_CODE, " ");
}

// webCitations keeps the citations that carry a usable [n] marker, first one wins
// per index (the backend can emit one citation per RAG chunk, but web sources are
// already unique per index).
function indexCitations(citations: Citation[]): Map<number, Citation> {
  const byIndex = new Map<number, Citation>();
  for (const citation of citations) {
    if (typeof citation.index !== "number" || citation.index <= 0) continue;
    if (!byIndex.has(citation.index)) byIndex.set(citation.index, citation);
  }
  return byIndex;
}

export function assignDisplayNumbers(
  content: string,
  citations?: Citation[],
  options?: {
    // Keep gathered-but-not-yet-cited sources in `ordered`, after the cited ones.
    //
    // Set this while an answer is streaming. Sources are delivered ahead of the
    // deltas that cite them, so without it the list is the full gathered set
    // (via the uncited fallback below) right up until the model writes its first
    // marker — at which point it collapses to that single cited source and then
    // grows back one icon at a time. Measured on a live turn, the favicon row went
    // 7 → 1 → 2 → … → 7, which reads as the sources vanishing mid-answer.
    //
    // Keeping the uncited ones makes the streamed list grow-only. The narrowing to
    // cited-only happens once, when the message settles.
    includeUncited?: boolean;
  },
): DisplayNumbering {
  const empty: DisplayNumbering = { display: new Map(), ordered: [] };
  if (citations === undefined || citations.length === 0) return empty;

  const byIndex = indexCitations(citations);
  if (byIndex.size === 0) return empty;

  const display = new Map<number, number>();
  const ordered: Citation[] = [];
  const text = stripCode(content);

  MARKER.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = MARKER.exec(text)) !== null) {
    const index = Number(match[1]);
    // An unknown marker (out of range, or a source not yet delivered) consumes no
    // display number — it stays inert text and cannot shift the numbering.
    const citation = byIndex.get(index);
    if (citation === undefined || display.has(index)) continue;
    display.set(index, display.size + 1);
    ordered.push(citation);
  }

  const remaining = [...byIndex.values()]
    .filter((citation) => !display.has(citation.index ?? 0))
    .sort((a, b) => (a.index ?? 0) - (b.index ?? 0));

  // Nothing cited: fall back to every gathered source, in discovery order. Without
  // this the Sources row would vanish on the ~half of answers that cite nothing,
  // losing all visibility into what was searched.
  if (ordered.length === 0) return { display: new Map(), ordered: remaining };

  // Uncited sources carry no display number (nothing in the prose points at them),
  // so they render unnumbered after the cited ones.
  if (options?.includeUncited === true) {
    return { display, ordered: [...ordered, ...remaining] };
  }
  return { display, ordered };
}
