// Rehype-Plugin: teilt prosehaltige Text-Nodes in Segment-<span>s auf, damit
// neu eintreffender Streaming-Text segmentweise von dunkel auf normal faden
// kann (siehe index.css .ui-stream-seg / @keyframes ui-stream-fade).
//
// Muss NACH rehype-highlight laufen und steigt nicht in code/pre ab, damit die
// Highlight-Spans nicht doppelt gewrappt werden.

const SKIP_TAGS = new Set(["pre", "code"]);

// Grobe „Segment/Zeile"-Granularität: Wörter (inkl. nachfolgendem Whitespace)
// zu Segmenten gruppieren; Segment beenden nach Satz-/Klausel-Zeichen oder
// sobald es lang genug ist. Bewusst gröber als pro Wort.
const MAX_SEG_CHARS = 28;

function splitIntoSegments(value: string): string[] {
  // Tokens inkl. anhängendem Whitespace erhalten, damit Spacing bleibt.
  const tokens = value.match(/\S+\s*|\s+/g);
  if (!tokens) return [value];

  const segments: string[] = [];
  let current = "";
  for (const tok of tokens) {
    current += tok;
    const trimmed = tok.trimEnd();
    const endsClause = /[.!?,;:—)\]]$/.test(trimmed);
    if (endsClause || current.length >= MAX_SEG_CHARS) {
      segments.push(current);
      current = "";
    }
  }
  if (current !== "") segments.push(current);
  return segments;
}

type HastNode = {
  type: string;
  tagName?: string;
  value?: string;
  children?: HastNode[];
  properties?: Record<string, unknown>;
};

function wrapTextNode(value: string): HastNode[] {
  return splitIntoSegments(value).map((seg) => ({
    type: "element",
    tagName: "span",
    properties: { className: ["ui-stream-seg"] },
    children: [{ type: "text", value: seg }],
  }));
}

function transform(node: HastNode): void {
  if (!node.children) return;
  const next: HastNode[] = [];
  for (const child of node.children) {
    if (child.type === "text" && typeof child.value === "string" && child.value.trim() !== "") {
      next.push(...wrapTextNode(child.value));
    } else {
      if (child.type === "element" && child.tagName && SKIP_TAGS.has(child.tagName)) {
        // code/pre unangetastet lassen
        next.push(child);
        continue;
      }
      transform(child);
      next.push(child);
    }
  }
  node.children = next;
}

export function rehypeStreamFade() {
  return (tree: HastNode) => {
    transform(tree);
  };
}
