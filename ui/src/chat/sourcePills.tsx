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
// Cap how many distinct source pills render inline. MiMo tends to dump every
// citation in one cluster at the end of the answer, so an uncapped render is a
// wall of pills that just repeats the footer Sources row. Beyond the cap, markers
// are dropped from the prose — every source still appears in the sidebar.
const MAX_INLINE_PILLS = 3;
// The custom element the plugin emits; ProseMarkdown maps it to <SourcePill>.
export const SOURCE_PILL_TAG = "citepill";

const PILL_CLASS =
  "ui-source-pill mx-0.5 inline-flex items-center rounded-full bg-[#363632] px-2 py-0.5 align-baseline font-sans text-[0.75rem] leading-[1.45rem] transition-colors hover:bg-[#44443f]";

type Ctx = {
  sources: SourceMap;
  shown: Set<number>; // the first MAX_INLINE_PILLS distinct cited indices
  emitted: Set<number>; // indices already rendered (dedupe across the message)
};

// rehypeSourcePills replaces [n] citation markers in prose with inline pills for
// the first MAX_INLINE_PILLS distinct cited sources; markers for any further
// (or repeated) sources are dropped from the text — they remain in the sidebar.
// Markers whose number is not a known source (out of range / hallucinated) are
// left as plain text, and only complete "[n]" tokens are rewritten (a partial
// "[1" streams through untouched).
export function rehypeSourcePills(sources: SourceMap) {
  return (tree: Root) => {
    const cited = collectCitedIndices(tree.children, sources);
    if (cited.length === 0) return;
    walk(tree.children, {
      sources,
      shown: new Set(cited.slice(0, MAX_INLINE_PILLS)),
      emitted: new Set(),
    });
  };
}

// collectCitedIndices returns the ordered, de-duplicated list of source indices
// actually cited in the prose, so the cap applies per message rather than per
// text node.
function collectCitedIndices(children: RootContent[], sources: SourceMap): number[] {
  const seen = new Set<number>();
  const order: number[] = [];
  const visit = (nodes: RootContent[]) => {
    for (const child of nodes) {
      if (child.type === "element") {
        if (SKIP_TAGS.has(child.tagName)) continue;
        visit(child.children);
      } else if (child.type === "text") {
        MARKER.lastIndex = 0;
        let m: RegExpExecArray | null;
        while ((m = MARKER.exec(child.value)) !== null) {
          const n = Number(m[1]);
          if (sources.has(n) && !seen.has(n)) {
            seen.add(n);
            order.push(n);
          }
        }
      }
    }
  };
  visit(children);
  return order;
}

function pillElement(ref: WebSourceRef): ElementContent {
  return {
    type: "element",
    tagName: SOURCE_PILL_TAG,
    properties: { href: ref.url },
    children: [{ type: "text", value: ref.label }],
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
    if (ref === undefined) continue; // unknown marker -> keep as literal text
    // Emit the text between the previous marker and this one.
    if (match.index > last) out.push({ type: "text", value: value.slice(last, match.index) });
    last = match.index + match[0].length;
    changed = true;
    // Render a pill only for one of the first-N distinct sources, once; all other
    // markers (overflow or repeats) are removed from the prose.
    if (ctx.shown.has(n) && !ctx.emitted.has(n)) {
      out.push(pillElement(ref));
      ctx.emitted.add(n);
    }
  }
  if (!changed) return null;
  if (last < value.length) out.push({ type: "text", value: value.slice(last) });
  return out;
}

// SourcePill renders an inline, clickable citation pill next to the prose it
// backs. Mirrors the prompt-classifier pill styling (SharedPill / MessageMetrics)
// but as a link. Falls back to its text if href is missing.
export function SourcePill({ href, children }: { href?: unknown; children?: ReactNode }) {
  if (typeof href !== "string" || href === "") return <>{children}</>;
  return (
    // The color/underline live in CSS (.ui-source-pill) so they outrank the
    // ".ui-markdown a" link styling this pill renders inside; the layout utilities
    // here aren't contested by that rule.
    <a href={href} target="_blank" rel="noreferrer noopener" className={PILL_CLASS}>
      {children}
    </a>
  );
}
