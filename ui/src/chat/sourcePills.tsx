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
// Markers standing together form a run, which renders ascending and with each
// source once — see emitRun. Repeats elsewhere in the message are kept: the same
// source legitimately backs several different claims, and per-sentence attribution
// is the whole point.
//
// A run reaches only within one text node. Adjacency is not tracked across nodes:
// markdown splits "[1]**bold**[2]" into three, and carrying the state across made
// the second marker look like it abutted the first — silently deleting the marker
// in "[1]**bold**[1]", where the two back genuinely different claims. The same
// applied across paragraphs, list items and table cells.
export function rehypeSourcePills(sources: SourceMap, display: DisplayMap) {
  return (tree: Root) => {
    walk(tree.children, { sources, display });
  };
}

// The punctuation a marker may be tightened against, once hoistPunctuation has moved
// it in front. A period or comma sits on the baseline and leaves the whole upper half
// of its advance empty, so the marker's normal left margin reads as a hole between
// the two. Deliberately not "!" or "?" — those are full-height and already fill that
// space, and pulling a plate against them crowds the line. ";" and ":" carry an
// upper mark for the same reason and are left out too.
const TIGHTENABLE_PUNCTUATION = /[.,]$/;

function pillElement(
  ref: WebSourceRef,
  shown: number,
  tight: boolean,
  run: string,
): ElementContent {
  return {
    type: "element",
    tagName: SOURCE_PILL_TAG,
    properties: {
      // A data attribute, because react-markdown normalizes properties through
      // property-information: a bespoke "tight" prop is dropped on the way to the
      // component, and className is owned by the renderer.
      dataTight: tight ? "true" : undefined,
      // Every number of the run this marker belongs to — see emitRun. A lone marker
      // carries just its own. The marker's *own* number needs no attribute: it is the
      // element's text, which is what the hover highlight matches on.
      dataRun: run,
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

// One marker of a run, resolved to what it renders as.
type RunMarker = { shown: number; ref: WebSourceRef };

// A run continues while the markers are separated by nothing or by blanks — the
// same span hoistPunctuation treats as one unit, and no wider: a newline or any
// prose ends the run, because the markers then back separate claims.
const RUN_SEPARATOR = /^[ \t]*$/;

// emitRun renders one cluster of markers: ascending by display number, each source
// once. A cluster is a citation list, and every numbered citation style prints one
// in order — the model emits "[3][1]" whenever it recalls the second source first,
// and "3 1" reads as a rendering fault. Sorting is confined to the cluster, so the
// order of the claims themselves is never touched.
//
// The blanks between the markers are dropped rather than re-emitted: walk() trims
// them away in front of every marker anyway, and a deduplicated run has fewer gaps
// than it started with.
//
// Every marker of the run is stamped with the run's full number list, because a run
// is one citation to the reader — "…in both specifications and schedules.²⁶" cites
// two sources for one claim, and clicking either plate has to select both. Without
// this the run's other sources stay unmarked and the drawer looks like it missed one.
function emitRun(
  out: ElementContent[],
  run: RunMarker[],
  tight: boolean,
): void {
  const ordered = [...run].sort((a, b) => a.shown - b.shown);
  const numbers = [...new Set(ordered.map((marker) => marker.shown))];
  const group = numbers.join(",");
  let previous: number | null = null;
  for (const marker of ordered) {
    // Sorting has brought any repeat of one source together: the model writes
    // "[1][1]" for a single claim, which should read as one number.
    if (marker.shown === previous) continue;
    out.push(
      pillElement(marker.ref, marker.shown, tight && previous === null, group),
    );
    previous = marker.shown;
  }
}

function splitMarkers(node: Text, ctx: Ctx): ElementContent[] | null {
  const value = hoistPunctuation(node.value, ctx);
  if (!value.includes("[")) return null;
  MARKER.lastIndex = 0;
  const out: ElementContent[] = [];
  let last = 0;
  let changed = false;
  // The run being collected, and whether it opened directly behind clause
  // punctuation ("benchmarks.²"). Run state is per node — see rehypeSourcePills.
  let run: RunMarker[] = [];
  let runTight = false;
  const flush = () => {
    if (run.length === 0) return;
    emitRun(out, run, runTight);
    run = [];
  };
  let match: RegExpExecArray | null;
  while ((match = MARKER.exec(value)) !== null) {
    const n = Number(match[1]);
    const ref = ctx.sources.get(n);
    const shown = ctx.display.get(n);
    // Unknown marker, or a source carrying no display number (never counted as
    // cited) -> keep the literal text rather than mis-linking it. It stays in the
    // gap ahead of the next marker, which therefore opens a new run: an unknown
    // marker splits a cluster instead of being sorted around.
    if (ref === undefined || shown === undefined) continue;
    const gap = value.slice(last, match.index);
    if (run.length === 0 || !RUN_SEPARATOR.test(gap)) {
      // Close the previous run, then emit the prose that separates it from this one.
      flush();
      if (gap !== "") out.push({ type: "text", value: gap });
      runTight = TIGHTENABLE_PUNCTUATION.test(value.slice(0, match.index));
    }
    run.push({ shown, ref });
    last = match.index + match[0].length;
    changed = true;
  }
  flush();
  if (!changed) return null;
  if (last < value.length) out.push({ type: "text", value: value.slice(last) });
  return out;
}

// SourcesOpener carries the "open the Sources drawer" callback down to the markers
// embedded in the prose. The drawer's state belongs to the message (AssistantMessage
// owns it, MessageCitations renders it), and the markers are rendered by
// react-markdown several levels below with no prop path to it.
//
// A surface that has no drawer must provide nothing, so its markers fall back to
// linking out. This is a rule the provider's callers have to keep — the context has
// no way to tell whether anything downstream actually renders a drawer, and a marker
// that reports a click nobody listens for is a dead button with its link removed.
// MessageBubble skips both providers when publicView is set, for exactly that reason.
const SourcesOpener = createContext<((numbers: number[]) => void) | null>(null);

export function SourcesOpenerProvider({
  onOpen,
  children,
}: {
  onOpen(numbers: number[]): void;
  children: ReactNode;
}) {
  return <SourcesOpener value={onOpen}>{children}</SourcesOpener>;
}

// PillHighlight is the drawer talking back to the prose: which sources are pinned
// (the reader clicked a marker) and which one is under the cursor in the source
// list. Both travel by context for the same reason the opener does — the markers
// sit several levels down inside react-markdown's output, with no prop path.
export type PillHighlight = {
  /** Display numbers of the current selection. */
  selected: ReadonlySet<number>;
  /** The source row being hovered in the drawer, if any. */
  hovered?: number;
};

const NOTHING_HIGHLIGHTED: PillHighlight = { selected: new Set() };
const PillHighlightContext = createContext<PillHighlight>(NOTHING_HIGHLIGHTED);

export function PillHighlightProvider({
  highlight,
  children,
}: {
  highlight: PillHighlight;
  children: ReactNode;
}) {
  return (
    <PillHighlightContext value={highlight}>{children}</PillHighlightContext>
  );
}

// runNumbers reads the run list a marker was stamped with. A marker rendered before
// the attribute existed — or by a surface that skips the plugin — falls back to its
// own number, so a click still selects something.
function runNumbers(run: unknown, shown: number): number[] {
  if (typeof run !== "string" || run === "")
    return Number.isNaN(shown) ? [] : [shown];
  const numbers = run
    .split(",")
    .map(Number)
    .filter((n) => !Number.isNaN(n));
  return numbers.length > 0 ? numbers : [shown];
}

// SourcePill renders the inline citation number. Where a Sources drawer exists the
// marker is a button that opens it — a citation's destination is the source list,
// not a new tab, so a click never navigates away from the answer. `title` carries
// the site name, which left the prose when the marker became numeric.
export function SourcePill({
  href,
  title,
  children,
  ...rest
}: {
  href?: unknown;
  title?: unknown;
  children?: ReactNode;
  "data-tight"?: unknown;
  "data-run"?: unknown;
}) {
  const openSources = useContext(SourcesOpener);
  const highlight = useContext(PillHighlightContext);
  const label = typeof title === "string" && title !== "" ? title : undefined;
  const shown = Number(String(children ?? ""));
  // Set by the plugin when the marker sits directly behind a period or comma.
  const classes = [PILL_CLASS];
  if (rest["data-tight"] !== undefined) classes.push("ui-source-pill-tight");
  // Pinned and hovered are separate looks on purpose — see .ui-source-card-selected
  // in index.css. A marker can be both, and neither cancels the other.
  if (highlight.selected.has(shown)) classes.push("ui-source-pill-active");
  if (highlight.hovered === shown) classes.push("ui-source-pill-linked");
  const className = classes.join(" ");

  if (openSources !== null)
    return (
      <button
        type="button"
        className={className}
        title={label}
        aria-label={label}
        onClick={() => openSources(runNumbers(rest["data-run"], shown))}
      >
        {children}
      </button>
    );

  // No drawer on this surface. A web source still has somewhere to go; a citation of
  // an uploaded document (no href) has nothing, and renders as plain text with the
  // same marker styling so it reads identically.
  if (typeof href !== "string" || href === "")
    return (
      <span className={className} title={label} aria-label={label}>
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
      className={className}
    >
      {children}
    </a>
  );
}
