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
  display: ReadonlyMap<number, number>;
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

// The numbering of an answer that cites nothing. One shared, read-only instance:
// the map is a prop of every prose block, so a fresh empty one per call would
// re-parse markdown that has not changed.
const NO_NUMBERING: DisplayNumbering = Object.freeze({
  display: new Map<number, number>(),
});

function sameDisplay(
  a: ReadonlyMap<number, number>,
  b: ReadonlyMap<number, number>,
): boolean {
  if (a.size !== b.size) return false;
  for (const [index, shown] of a) {
    if (b.get(index) !== shown) return false;
  }
  return true;
}

// `previous` is the numbering of the last call for the same answer. It is handed
// back, same identity, when the text grew without citing a new source — which
// mid-stream is nearly every delta.
export function assignDisplayNumbers(
  content: string,
  citations?: Citation[],
  previous?: DisplayNumbering,
): DisplayNumbering {
  if (citations === undefined || citations.length === 0) return NO_NUMBERING;

  const byIndex = indexCitations(citations);
  if (byIndex.size === 0) return NO_NUMBERING;

  const display = new Map<number, number>();
  const text = stripCode(citeSourceURLs(content, byIndex));
  MARKER.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = MARKER.exec(text)) !== null) {
    const index = Number(match[1]);
    // An unknown marker (out of range, or a source not yet delivered) consumes no
    // display number — it stays inert text and cannot shift the numbering.
    if (!byIndex.has(index) || display.has(index)) continue;
    display.set(index, display.size + 1);
  }
  if (display.size === 0) return NO_NUMBERING;
  if (previous !== undefined && sameDisplay(previous.display, display))
    return previous;
  return { display };
}

// stableDisplayNumbers returns an assignDisplayNumbers that feeds each result
// into the next call, for a caller that numbers the same growing answer over and
// over (the live turn).
export function stableDisplayNumbers(): (
  content: string,
  citations?: Citation[],
) => DisplayNumbering {
  let previous: DisplayNumbering | undefined;
  return (content, citations) => {
    previous = assignDisplayNumbers(content, citations, previous);
    return previous;
  };
}

// A bracket holding nothing but URLs: "[https://a]", "[<https://a>, https://b]".
// Not an image ("![…]"), an escaped bracket, a link's text ("[…](…)", or nested
// in one: "[see [https://a]](…)") or a reference definition ("[…]: …").
const BRACKETED = /(?<![!\\])\[([^[\]\n]+)\](?![(:\]])/g;
const URL_PART = /^<?(https?:\/\/[^\s<>]+?)>?[.,;:]*$/;

type Linked = { url?: string };
type Page = { index: number; params: URLSearchParams };
type URLIndex = { exact: Map<string, number>; pages: Map<string, Page[]> };

// splitURL separates a URL's page (origin and path, no trailing slash) from its
// query. The fragment is dropped.
function splitURL(
  raw: string,
): { page: string; params: URLSearchParams } | undefined {
  try {
    const url = new URL(raw);
    return {
      page: url.origin + url.pathname.replace(/\/+$/, ""),
      params: url.searchParams,
    };
  } catch {
    return undefined;
  }
}

function indexURLs(sources: ReadonlyMap<number, Linked>): URLIndex {
  const exact = new Map<string, number>();
  const pages = new Map<string, Page[]>();
  for (const [index, source] of sources) {
    const url = source.url?.trim();
    if (url === undefined || url === "") continue;
    if (!exact.has(url)) exact.set(url, index);
    const parts = splitURL(url);
    if (parts === undefined) continue;
    const list = pages.get(parts.page) ?? [];
    list.push({ index, params: parts.params });
    pages.set(parts.page, list);
  }
  return { exact, pages };
}

// lookupURL resolves a URL the model copied. Past an exact match, it accepts a
// copy that dropped query parameters (a timestamp "?t=42") but never one that
// changed or added one: "watch?v=B" is not the source "watch?v=A". A copy that
// fits several sources (two moments of one video) is ambiguous and stays as written.
function lookupURL(urls: URLIndex, url: string): number | undefined {
  const exact = urls.exact.get(url);
  if (exact !== undefined) return exact;
  const parts = splitURL(url);
  if (parts === undefined) return undefined;
  const fits = new Set<number>();
  for (const page of urls.pages.get(parts.page) ?? []) {
    let subset = true;
    for (const [key, value] of parts.params) {
      if (!page.params.getAll(key).includes(value)) subset = false;
    }
    if (subset) fits.add(page.index);
  }
  return fits.size === 1 ? [...fits][0] : undefined;
}

// citeSourceURLs rewrites a bracketed URL of one of the answer's sources into
// that source's [n] marker. Told by a tool to cite the links it gave, the model
// once wrote "[https://…]" where the marker belongs, and the raw link stayed in
// the prose. A bracket stays as written unless every URL in it is a known
// source, and code is never touched. Numbering, pills and the copied text all
// read the rewritten text, so they agree.
export function citeSourceURLs(
  content: string,
  sources?: ReadonlyMap<number, Linked>,
): string {
  if (sources === undefined || sources.size === 0) return content;
  if (!/\[\s*<?https?:\/\//.test(content)) return content;
  const urls = indexURLs(sources);
  if (urls.exact.size === 0) return content;
  // Matched on the code-blanked copy, which keeps every offset, then cut from
  // the original.
  const masked = stripCode(content);
  let out = "";
  let last = 0;
  BRACKETED.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = BRACKETED.exec(masked)) !== null) {
    const markers = bracketMarkers(match[1], urls);
    if (markers === null) continue;
    out += content.slice(last, match.index) + markers;
    last = match.index + match[0].length;
  }
  return last === 0 ? content : out + content.slice(last);
}

// bracketMarkers reads the bracket as one URL first, so a comma inside a URL
// ("?ids=1,2") does not split it, then as a list.
function bracketMarkers(inner: string, urls: URLIndex): string | null {
  const whole = markerOf(inner.trim(), urls);
  if (whole !== null) return whole;
  const parts = inner.split(/[\s,;]+/).filter((part) => part !== "");
  if (parts.length < 2) return null;
  let markers = "";
  for (const part of parts) {
    const marker = markerOf(part, urls);
    if (marker === null) return null;
    markers += marker;
  }
  return markers;
}

function markerOf(part: string, urls: URLIndex): string | null {
  const url = URL_PART.exec(part)?.[1];
  const index = url === undefined ? undefined : lookupURL(urls, url);
  return index === undefined ? null : `[${index}]`;
}
