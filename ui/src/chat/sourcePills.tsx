import type { ReactNode } from "react";
import type { ElementContent, Root, RootContent, Text } from "hast";

import type { Citation } from "../api";

// WebSourceRef is one citable web source: the link and its display label (the
// site name derived by the backend, e.g. "Truefoundry").
export type WebSourceRef = { url: string; label: string };

// SourceMap maps a [n] citation index to its web source, built from a message's
// persisted citations. Used only for the inline pills; the sidebar reads the full
// citation records directly.
export type SourceMap = Map<number, WebSourceRef>;

// webSourceMap collects the web-search citations (those carrying a url + index)
// into an index -> source map, so inline [n] markers can resolve to pills.
// Returns undefined when there are no web sources, so callers can cheaply skip
// the rehype pass for answers that used no web tools.
export function webSourceMap(citations?: Citation[]): SourceMap | undefined {
  if (citations === undefined || citations.length === 0) return undefined;
  const map: SourceMap = new Map();
  for (const citation of citations) {
    if (
      typeof citation.url === "string" &&
      citation.url !== "" &&
      typeof citation.index === "number" &&
      citation.index > 0 &&
      !map.has(citation.index)
    ) {
      map.set(citation.index, { url: citation.url, label: citation.filename });
    }
  }
  return map.size > 0 ? map : undefined;
}

const MARKER = /\[(\d+)\]/g;
// Never rewrite markers inside code, pre or existing links — a "[1]" there is
// code/text, not a citation.
const SKIP_TAGS = new Set(["code", "pre", "a"]);
// The custom element the plugin emits; ProseMarkdown maps it to <SourcePill>.
export const SOURCE_PILL_TAG = "citepill";

// A raised numeral rather than a chip. The chip form carried the site name, which
// was heavy enough that only three could render before the prose became a wall of
// pills — the reason a per-message cap used to exist. Measured marker positions
// show no end-clustering (9% in the final fifth against 20% for a uniform spread)
// and a median of 4 cited sources per answer, so every marker now renders.
const PILL_CLASS =
  "ui-source-pill ml-px inline-block align-baseline font-sans text-[0.7rem] font-semibold tabular-nums transition-colors";

type Ctx = {
  sources: SourceMap;
  display: DisplayMap;
  last: number | null; // previous emitted index, for immediate-repeat dedupe
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
export function rehypeSourcePills(sources: SourceMap, display: DisplayMap) {
  return (tree: Root) => {
    walk(tree.children, { sources, display, last: null });
  };
}

function pillElement(ref: WebSourceRef, shown: number): ElementContent {
  return {
    type: "element",
    tagName: SOURCE_PILL_TAG,
    properties: {
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
  let match: RegExpExecArray | null;
  while ((match = MARKER.exec(value)) !== null) {
    const n = Number(match[1]);
    const ref = ctx.sources.get(n);
    const shown = ctx.display.get(n);
    // Unknown marker, or a source carrying no display number (never counted as
    // cited) -> keep the literal text rather than mis-linking it.
    if (ref === undefined || shown === undefined) continue;
    // A repeat collapses only when it directly abuts the previous marker: the
    // model writes "[1][1]" for one claim, which should read as one number. Once
    // any prose intervenes the repeat is backing a separate claim, and dropping it
    // would strip that sentence of its attribution.
    const adjacent = ctx.last === n && match.index === last;
    // Emit the text between the previous marker and this one.
    if (match.index > last)
      out.push({ type: "text", value: value.slice(last, match.index) });
    last = match.index + match[0].length;
    changed = true;
    if (!adjacent) out.push(pillElement(ref, shown));
    ctx.last = n;
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
  if (typeof href !== "string" || href === "") return <>{children}</>;
  const label = typeof title === "string" && title !== "" ? title : undefined;
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
