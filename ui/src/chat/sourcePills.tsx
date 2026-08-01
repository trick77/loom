import type { ReactNode } from "react";
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

// A bracketed numeral — [1] — rather than a chip or a bare superscript. The chip
// carried the site name, heavy enough that only three could render before the prose
// became a wall of pills (the reason a per-message cap used to exist). A bare
// raised numeral fixed the weight but went too far the other way: at 0.7rem and
// unpunctuated it disappears into the text.
//
// Brackets also remove a real ambiguity. Adjacent bare numerals ran together —
// "[12][13]" rendered as "1213", one number — which needed a separator comma
// spliced between them. Brackets delimit themselves, so that hack is gone.
const PILL_CLASS =
  "ui-source-pill inline-block align-baseline font-sans text-[0.78rem] font-semibold tabular-nums transition-colors";

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
    children: [{ type: "text", value: `[${shown}]` }],
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
        i += replaced.length - 1;
      }
    }
  }
}

function splitMarkers(node: Text, ctx: Ctx): ElementContent[] | null {
  const value = node.value;
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

// SourcePill renders the inline citation number as a link to the source it backs.
// Falls back to its text if href is missing. `title` carries the site name, which
// left the prose when the marker became numeric.
export function SourcePill({
  href,
  title,
  children,
}: {
  href?: unknown;
  title?: unknown;
  children?: ReactNode;
}) {
  const label = typeof title === "string" && title !== "" ? title : undefined;
  // No href means a citation of an uploaded document — numbered like a web source,
  // but with nothing to link to. Render it with the same marker styling so it reads
  // identically to a web citation.
  if (typeof href !== "string" || href === "")
    return (
      <span className={PILL_CLASS} title={label} aria-label={label}>
        {children}
      </span>
    );
  return (
    // The color/underline live in CSS (.ui-source-pill) so they outrank the
    // ".ui-markdown a" link styling this pill renders inside; the layout utilities
    // here aren't contested by that rule.
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
