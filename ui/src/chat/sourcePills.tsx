import type { ReactNode } from "react";
import type { ElementContent, Root, RootContent, Text } from "hast";

import type { Citation } from "../api";

// WebSourceRef is one citable web source: the link and its display label (the
// site name derived by the backend, e.g. "Truefoundry").
export type WebSourceRef = { url: string; label: string };

// SourceMap maps a [n] citation index to its web source, built from a message's
// citations (or the live web_sources stream event).
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

// rehypeSourcePills replaces [n] citation markers in prose text with <citepill>
// elements for any n present in `sources`. Markers whose number is not a known
// source (out of range, or the model hallucinated one) are left as plain text.
// It only rewrites complete "[n]" tokens, so a partially-streamed "[1" stays as
// text until the closing bracket arrives — no flicker of broken pills.
export function rehypeSourcePills(sources: SourceMap) {
  return (tree: Root) => {
    walk(tree.children, sources);
  };
}

function walk(children: RootContent[], sources: SourceMap): void {
  for (let i = 0; i < children.length; i++) {
    const child = children[i];
    if (child.type === "element") {
      if (SKIP_TAGS.has(child.tagName)) continue;
      walk(child.children, sources);
    } else if (child.type === "text") {
      const replaced = splitMarkers(child, sources);
      if (replaced !== null) {
        children.splice(i, 1, ...replaced);
        i += replaced.length - 1;
      }
    }
  }
}

function splitMarkers(node: Text, sources: SourceMap): ElementContent[] | null {
  const value = node.value;
  if (!value.includes("[")) return null;
  MARKER.lastIndex = 0;
  const out: ElementContent[] = [];
  let last = 0;
  let changed = false;
  let match: RegExpExecArray | null;
  while ((match = MARKER.exec(value)) !== null) {
    const ref = sources.get(Number(match[1]));
    if (ref === undefined) continue; // unknown marker -> keep as literal text
    const adjacent = match.index === last;
    const prev = out[out.length - 1];
    // Collapse a run of the same source ("[1][1]") into one pill, matching how
    // Claude renders a citation cluster.
    if (
      adjacent &&
      prev !== undefined &&
      prev.type === "element" &&
      prev.tagName === SOURCE_PILL_TAG &&
      prev.properties?.href === ref.url
    ) {
      last = match.index + match[0].length;
      changed = true;
      continue;
    }
    if (match.index > last) {
      out.push({ type: "text", value: value.slice(last, match.index) });
    }
    out.push({
      type: "element",
      tagName: SOURCE_PILL_TAG,
      properties: { href: ref.url },
      children: [{ type: "text", value: ref.label }],
    });
    last = match.index + match[0].length;
    changed = true;
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
    <a
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      className="ui-source-pill mx-0.5 inline-flex items-center rounded-full bg-[#363632] px-2 py-0.5 align-baseline font-sans text-[0.75rem] leading-[1.45rem] transition-colors hover:bg-[#44443f]"
    >
      {children}
    </a>
  );
}
