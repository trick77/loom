import type { Citation } from "../api";

// The backend stamps every source with a stable [n] — documents first, then web
// pages continuing the same sequence — and that is the number the model sees and
// emits. It is a poor *display* number: the model typically cites 4 of 10 gathered
// sources, so an answer can open with [3] while sources it never used sit on top.
//
// assignDisplayNumbers derives a presentation numbering from the answer text:
// 1, 2, 3… in the order the model first cites each source. The persisted
// citation.index is never mutated — it is the model's contract, the number stamped
// into the tool output it reads — so nothing in the DB or backend changes and
// historical messages renumber correctly with no migration.
//
// The mapping is append-only: a display number, once assigned, never changes as
// more text arrives. That is what makes it safe to run against a partial answer
// mid-stream — pills never reshuffle, they only get appended to.
//
// This module answers only "what number does each cited source show?". Which
// sources appear under the answer is MessageCitations' job — it needs the full
// citation list (every RAG chunk, not one per document) to count excerpts.

export type DisplayNumbering = {
  // persisted citation.index -> display number
  display: Map<number, number>;
};

const MARKER = /\[(\d+)\]/g;
// Code regions, in the order they must be removed. Markers inside code are array
// indices (arr[0], argv[2]), not citations. This has to agree with the pill
// plugin's SKIP_TAGS (code/pre) or the two disagree about what is a citation:
// a marker counted here but not pilled still consumes a display number, so the
// genuinely cited source renders under the wrong one. Hence indented blocks and
// ~~~ fences, which markdown also renders as <pre>/<code>.
const CODE_REGIONS: RegExp[] = [
  /^( {4}|\t)[^\n]*$/gm, // indented code block lines
  /```[\s\S]*?(?:```|$)/g, // ``` fence (closed or still streaming)
  /~~~[\s\S]*?(?:~~~|$)/g, // ~~~ fence
  /(`+)[^\n]*?\1/g, // inline span, any backtick run length
  /`+[^\n]*$/gm, // unclosed inline span (mid-stream)
];

// stripCode blanks out code regions, replacing each with spaces of the same length
// so the surrounding text keeps its offsets. Only marker *order* is used today,
// but preserving position keeps the function honest if that changes.
function stripCode(content: string): string {
  let out = content;
  for (const region of CODE_REGIONS) {
    out = out.replace(region, (match) => " ".repeat(match.length));
  }
  return out;
}

// indexCitations maps each [n] to a representative citation. First wins per index:
// several RAG chunks of one document share its marker, and any of them identifies
// the source for numbering purposes.
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
): DisplayNumbering {
  const display = new Map<number, number>();
  if (citations === undefined || citations.length === 0) return { display };

  const byIndex = indexCitations(citations);
  if (byIndex.size === 0) return { display };

  const text = stripCode(content);
  MARKER.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = MARKER.exec(text)) !== null) {
    const index = Number(match[1]);
    // An unknown marker (out of range, or a source not yet delivered) consumes no
    // display number — it stays inert text and cannot shift the numbering.
    if (!byIndex.has(index) || display.has(index)) continue;
    display.set(index, display.size + 1);
  }
  return { display };
}
