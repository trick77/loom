import { createContext, useContext, type ReactNode } from "react";
import type { ElementContent, Root, RootContent, Text } from "hast";

import type { Citation } from "../api";

// WebSourceRef is one citable source: its display label, plus a link when the
// source is a web page. Uploaded documents carry a marker but no url — they are
// cited the same way, and render as a marker that is not a link.
export type WebSourceRef = { url?: string; label: string };

// SourceMap maps a [n] citation index to its web source, built from a message's
// persisted citations. Used only for the inline pills; the sidebar reads the full
// citation records directly.
export type SourceMap = Map<number, WebSourceRef>;

// webSourceMap collects every citation carrying an [n] marker into an
// index -> source map, so inline markers can resolve. Both web pages and uploaded
// documents are numbered from one sequence by the backend; a document simply has
// no url. Returns undefined when nothing is numbered, so callers can cheaply skip
// the rehype pass for answers that cited nothing.
export function webSourceMap(citations?: Citation[]): SourceMap | undefined {
  if (citations === undefined || citations.length === 0) return undefined;
  const map: SourceMap = new Map();
  for (const citation of citations) {
    if (typeof citation.index !== "number" || citation.index <= 0) continue;
    if (map.has(citation.index)) continue;
    const url =
      typeof citation.url === "string" && citation.url !== ""
        ? citation.url
        : undefined;
    map.set(citation.index, { url, label: citation.filename });
  }
  return map.size > 0 ? map : undefined;
}

const MARKER = /\[(\d+)\]/g;
// Never rewrite markers inside code, pre or existing links — a "[1]" there is
// code/text, not a citation.
const SKIP_TAGS = new Set(["code", "pre", "a"]);
// The custom element the plugin emits; ProseMarkdown maps it to <SourcePill>.
export const SOURCE_PILL_TAG = "citepill";

// A bare numeral on a tinted plate — see .ui-source-pill in index.css, which owns
// every visual property so the marker and the sidebar's matching number cannot
// drift apart.
//
// The plate is what makes the numeral bare. Brackets were carrying two jobs: they
// kept a raised numeral visible, and they delimited adjacent markers so "[12][13]"
// could not read as "1213". The plate does both — it is its own delimiter, and it
// is visible without punctuation — while "[" and "]" cost ~9px inside every marker,
// enough that a run of them read as a spaced-out ribbon rather than as prose.
const PILL_CLASS = "ui-source-pill";

type Ctx = {
  sources: SourceMap;
  display: DisplayMap;
};

// DisplayMap maps a persisted citation index to the number shown to the reader —
// 1, 2, 3… in order of first citation. See sourceNumbering.ts.
export type DisplayMap = Map<number, number>;

// rehypeSourcePills replaces [n] citation markers in prose with the reader-facing
// number, linked to its source. Markers whose number is not a known source (out of
// range, or a source not yet delivered mid-stream) are left as plain text, and only
// complete "[n]" tokens are rewritten — a partial "[1" streams through untouched.
//
// Immediately repeated markers for the same source collapse to one. Repeats
// elsewhere in the message are kept: the same source legitimately backs several
// different claims, and per-sentence attribution is the whole point.
//
// "Immediately" means within one text node. Adjacency is not tracked across nodes:
// markdown splits "[1]**bold**[2]" into three, and carrying the state across made
// the second marker look like it abutted the first — silently deleting the marker
// in "[1]**bold**[1]", where the two back genuinely different claims. The same
// applied across paragraphs, list items and table cells.
export function rehypeSourcePills(sources: SourceMap, display: DisplayMap) {
  return (tree: Root) => {
    walk(tree.children, { sources, display });
  };
}

function pillElement(ref: WebSourceRef, shown: number): ElementContent {
  return {
    type: "element",
    tagName: SOURCE_PILL_TAG,
    properties: {
      // Absent for an uploaded document: SourcePill then renders the number as
      // plain (non-link) text rather than a dead anchor.
      href: ref.url,
      // The site name left the prose when the pill became numeric; keep it here so
      // hover and screen readers still identify the source.
      title: ref.label,
      "aria-label": ref.label,
    },
    children: [{ type: "text", value: String(shown) }],
  };
}

function walk(children: RootContent[], ctx: Ctx): void {
  for (let i = 0; i < children.length; i++) {
    const child = children[i];
    if (child.type === "element") {
      if (SKIP_TAGS.has(child.tagName)) continue;
      walk(child.children, ctx);
    } else if (child.type === "text") {
      const replaced = splitMarkers(child, ctx);
      if (replaced !== null) {
        children.splice(i, 1, ...replaced);
        // A marker has to hug the word it backs. The model writes "domain data [1]",
        // and only "[1]" is replaced — the space in front survives and reads as a
        // gap between the word and its marker.
        for (let j = 0; j < replaced.length; j++) {
          if (isPill(replaced[j])) trimBefore(children, i + j);
        }
        i += replaced.length - 1;
      }
    }
  }
}

function isPill(node: RootContent | ElementContent): boolean {
  return node.type === "element" && node.tagName === SOURCE_PILL_TAG;
}

// trimBefore removes the whitespace directly in front of the marker at `index`.
// The text ahead of it is usually the sibling text node, but while an answer
// streams rehypeStreamFade has already split the prose into <span> segments that
// keep their trailing space — so the space can also be the tail of the previous
// element. Descending into that element's last text node is what keeps the marker
// from jumping sideways when streaming ends and the segments disappear.
function trimBefore(children: RootContent[], index: number): void {
  const previous = children[index - 1];
  if (previous === undefined) return;
  const text = lastTextNode(previous);
  if (text === null) return;
  text.value = text.value.replace(/[ \t]+$/, "");
}

function lastTextNode(node: RootContent | ElementContent): Text | null {
  if (node.type === "text") return node;
  if (node.type !== "element" || SKIP_TAGS.has(node.tagName)) return null;
  for (let i = node.children.length - 1; i >= 0; i--) {
    const found = lastTextNode(node.children[i]);
    if (found !== null) return found;
  }
  return null;
}

// A run of markers, optionally spaced, followed by the punctuation that closes the
// clause: "[1][2]." or "[1] ,". Captured as one unit so the punctuation can move in
// front of the whole run rather than in front of its last marker.
const MARKER_RUN_BEFORE_PUNCTUATION = /((?:[ \t]*\[\d+\])+)[ \t]*([.,;:!?])/g;

// hoistPunctuation moves clause punctuation ahead of the markers that precede it.
// The model writes "on most benchmarks [2]." — the marker lands between the last
// word and the full stop, which leaves the sentence visibly unfinished and the
// period orphaned after a raised plate. Readers expect "benchmarks.²", the
// convention every print citation style uses.
//
// Only runs whose every marker resolves to a cited source are moved: an unknown
// "[7]" stays literal text, and rewriting the prose around it would corrupt a
// sentence the plugin has decided not to touch.
function hoistPunctuation(value: string, ctx: Ctx): string {
  return value.replace(
    MARKER_RUN_BEFORE_PUNCTUATION,
    (whole, run: string, punctuation: string) => {
      const numbers = run.match(/\d+/g) ?? [];
      const known = numbers.every((n) => ctx.display.has(Number(n)));
      return known ? `${punctuation}${run.trimStart()}` : whole;
    },
  );
}

function splitMarkers(node: Text, ctx: Ctx): ElementContent[] | null {
  const value = hoistPunctuation(node.value, ctx);
  if (!value.includes("[")) return null;
  MARKER.lastIndex = 0;
  const out: ElementContent[] = [];
  let last = 0;
  let changed = false;
  // Adjacency state is per node — see rehypeSourcePills.
  let previousIndex: number | null = null;
  let previousWasMarker = false;
  let match: RegExpExecArray | null;
  while ((match = MARKER.exec(value)) !== null) {
    const n = Number(match[1]);
    const ref = ctx.sources.get(n);
    const shown = ctx.display.get(n);
    // Unknown marker, or a source carrying no display number (never counted as
    // cited) -> keep the literal text rather than mis-linking it.
    if (ref === undefined || shown === undefined) continue;
    // Whether this marker directly abuts the previous one ("[1][2]" with nothing
    // between), as opposed to being separated by prose.
    const abuts = match.index === last && previousWasMarker;
    // A repeat collapses only when it abuts: the model writes "[1][1]" for a single
    // claim, which should read as one number. Once any prose intervenes the repeat
    // backs a separate claim, and dropping it would strip that sentence of its
    // attribution.
    const repeat = abuts && previousIndex === n;
    // Emit the text between the previous marker and this one.
    if (match.index > last)
      out.push({ type: "text", value: value.slice(last, match.index) });
    last = match.index + match[0].length;
    changed = true;
    if (!repeat) out.push(pillElement(ref, shown));
    previousIndex = n;
    previousWasMarker = true;
  }
  if (!changed) return null;
  if (last < value.length) out.push({ type: "text", value: value.slice(last) });
  return out;
}

// SourcesOpener carries the "open the Sources drawer" callback down to the markers
// embedded in the prose. The drawer's state belongs to the message (AssistantMessage
// owns it, MessageCitations renders it), and the markers are rendered by
// react-markdown several levels below with no prop path to it.
//
// A surface that has no drawer simply provides nothing: the public share view skips
// MessageCitations entirely, so its markers fall back to linking out.
const SourcesOpener = createContext<((index?: number) => void) | null>(null);

export function SourcesOpenerProvider({
  onOpen,
  children,
}: {
  onOpen(index?: number): void;
  children: ReactNode;
}) {
  return <SourcesOpener value={onOpen}>{children}</SourcesOpener>;
}

// SourcePill renders the inline citation number. Where a Sources drawer exists the
// marker is a button that opens it — a citation's destination is the source list,
// not a new tab, so a click never navigates away from the answer. `title` carries
// the site name, which left the prose when the marker became numeric.
export function SourcePill({
  href,
  title,
  children,
}: {
  href?: unknown;
  title?: unknown;
  children?: ReactNode;
}) {
  const openSources = useContext(SourcesOpener);
  const label = typeof title === "string" && title !== "" ? title : undefined;
  const shown = Number(String(children ?? ""));

  if (openSources !== null)
    return (
      <button
        type="button"
        className={PILL_CLASS}
        title={label}
        aria-label={label}
        onClick={() => openSources(Number.isNaN(shown) ? undefined : shown)}
      >
        {children}
      </button>
    );

  // No drawer on this surface. A web source still has somewhere to go; a citation of
  // an uploaded document (no href) has nothing, and renders as plain text with the
  // same marker styling so it reads identically.
  if (typeof href !== "string" || href === "")
    return (
      <span className={PILL_CLASS} title={label} aria-label={label}>
        {children}
      </span>
    );
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      title={label}
      aria-label={label}
      className={PILL_CLASS}
    >
      {children}
    </a>
  );
}
