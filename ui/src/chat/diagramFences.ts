// rehypeDiagramFences marks every ```mermaid block of an answer for CodeBlock
// (messages.tsx), which draws it instead of printing it.
//
// A fence is drawn only once its closing line is in: a half-written source
// draws a half picture, or a parse error, on every streamed token. While the
// answer still streams inside the fence the reader sees that a picture is on
// its way, not the source flashing by. A fence still open once the answer is
// over (a stream cut, a stored answer that ended inside one) stays the code it
// is, never a placeholder that waits for nothing.
//
// Whether the fence closed is read off the text: the parser ends an open
// fence at the end of the document, as it ends a closed one there, so only the
// block's own last line tells the two apart.

export const DIAGRAM_ATTRIBUTE = "dataDiagram";

export type DiagramMark = "draw" | "pending";

type HastNode = {
  type: string;
  tagName?: string;
  value?: string;
  properties?: Record<string, unknown>;
  children?: HastNode[];
  position?: { start: { offset?: number }; end: { offset?: number } };
};

export function rehypeDiagramFences(options: { streaming?: boolean } = {}) {
  const streaming = options.streaming ?? false;
  return (tree: HastNode, file: { value?: unknown }) => {
    const text = typeof file.value === "string" ? file.value : "";
    visit(tree, (pre) => {
      const code = pre.children?.find(
        (c) => c.type === "element" && c.tagName === "code",
      );
      if (code === undefined || !isMermaid(code)) return;
      const start = pre.position?.start.offset;
      const end = pre.position?.end.offset;
      if (start === undefined || end === undefined) return;
      const mark: DiagramMark | null = fenceClosed(text.slice(start, end))
        ? "draw"
        : streaming
          ? "pending"
          : null;
      if (mark !== null)
        pre.properties = { ...pre.properties, [DIAGRAM_ATTRIBUTE]: mark };
    });
  };
}

function visit(node: HastNode, onPre: (pre: HastNode) => void): void {
  for (const child of node.children ?? []) {
    if (child.type !== "element") continue;
    if (child.tagName === "pre") onPre(child);
    else visit(child, onPre);
  }
}

function isMermaid(code: HastNode): boolean {
  const names = code.properties?.className;
  return Array.isArray(names) && names.includes("language-mermaid");
}

/** fenceClosed says a fenced block's text, opening line to its end, holds its
 * closing line: the opening's character, at least as many of it, nothing
 * after. A list item's or a quote's prefix may stand in front of either. */
export function fenceClosed(block: string): boolean {
  const lines = block.split(/\r?\n/);
  if (lines.length < 2) return false;
  const open = /^[ \t>]*(`{3,}|~{3,})/.exec(lines[0]);
  const close = /^[ \t>]*(`{3,}|~{3,})[ \t]*$/.exec(lines[lines.length - 1]);
  if (open === null || close === null) return false;
  return close[1][0] === open[1][0] && close[1].length >= open[1].length;
}

/** diagramSource is the source a marked block holds, as the model wrote it. */
export function diagramSource(pre: HastNode): string {
  const code = pre.children?.find(
    (c) => c.type === "element" && c.tagName === "code",
  );
  return textOf(code).replace(/\n$/, "");
}

function textOf(node: HastNode | undefined): string {
  if (node === undefined) return "";
  if (node.type === "text") return node.value ?? "";
  return (node.children ?? []).map(textOf).join("");
}

/** diagramMark is how rehypeDiagramFences marked a block, if it did. */
export function diagramMark(pre: HastNode | undefined): DiagramMark | null {
  const mark = pre?.properties?.[DIAGRAM_ATTRIBUTE];
  return mark === "draw" || mark === "pending" ? mark : null;
}
