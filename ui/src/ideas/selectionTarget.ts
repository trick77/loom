// The attribute that marks an answer's prose as a place to capture a
// passage from: its value is the message id. Only assistant text blocks
// carry it (not trace panels, idea cards, the footer, a user's bubble).
export const PROSE_ATTR = "data-idea-prose";

// MIN_SELECTION is the shortest passage (trimmed, in runes) worth an idea.
export const MIN_SELECTION = 3;

// Places a selection may not start or end in: anything typed into.
const TYPED =
  'input, textarea, select, [contenteditable=""], [contenteditable="true"]';

// Rendered chrome inside the prose whose text is no part of the passage:
// citation markers (their number only), buttons (a code block's copy) and
// KaTeX's hidden MathML twin of every formula (the visible one stays).
const CHROME = ".ui-source-pill, button, .katex-mathml";

// Elements after which the passage breaks the line, as the answer reads.
const BLOCK =
  /^(P|DIV|LI|UL|OL|PRE|BLOCKQUOTE|H[1-6]|TABLE|TR|SECTION|ARTICLE|HR)$/;

// Table cells: a space between them, not glued together.
const CELL = /^(TD|TH)$/;

export type SelectionTarget = { messageId: string; text: string };

type Located = { range: Range; start: Element; messageId: string };

function elementOf(node: Node | null): Element | null {
  if (node === null) return null;
  return node instanceof Element ? node : node.parentElement;
}

function proseOf(node: Node | null): Element | null {
  return elementOf(node)?.closest(`[${PROSE_ATTR}]`) ?? null;
}

// endsRightAfter: a range end at offset 0 just past the prose, with nothing
// selected in between (a triple-click on the last paragraph ends there).
function endsRightAfter(prose: Element, range: Range): boolean {
  if (range.endOffset !== 0) return false;
  const gap = document.createRange();
  try {
    gap.setStart(prose, prose.childNodes.length);
    gap.setEnd(range.endContainer, 0);
  } catch {
    return false;
  }
  return !gap.collapsed && gap.toString().trim() === "";
}

// locate checks a selection's ends without reading its text (cheap enough
// for every selectionchange of a drag): both in the prose of ONE assistant
// message, neither in a field nor wholly inside a marker or button.
function locate(selection: Selection | null): Located | null {
  if (selection === null || selection.isCollapsed || selection.rangeCount === 0)
    return null;
  const range = selection.getRangeAt(0);
  const start = proseOf(range.startContainer);
  if (start === null) return null;
  const messageId = start.getAttribute(PROSE_ATTR) ?? "";
  const end = proseOf(range.endContainer);
  const sameMessage =
    end !== null
      ? end.getAttribute(PROSE_ATTR) === messageId
      : endsRightAfter(start, range);
  if (messageId === "" || !sameMessage) return null;
  // An end just past the prose lies outside it by design: only an end
  // inside counts.
  const ends =
    end === null
      ? [range.startContainer]
      : [range.startContainer, range.endContainer];
  for (const node of ends) {
    if (elementOf(node)?.closest(TYPED)) return null;
  }
  // Wholly inside a marker or a button: nothing of the passage (the clone
  // would hold its bare text, without the element to skip).
  if (elementOf(range.commonAncestorContainer)?.closest(CHROME)) return null;
  return { range, start, messageId };
}

/** selectionInAnswer: the selection could be a passage (no text read). */
export function selectionInAnswer(selection: Selection | null): boolean {
  return locate(selection) !== null;
}

// passageText reads the selected text the way the answer reads: without
// chrome, with line breaks after blocks and a space between table cells,
// indentation kept (code). When the range spans several text blocks, only
// text inside a prose block counts: a trace panel or card between two of
// them is not part of the passage.
function passageText(range: Range, withinOne: boolean): string {
  let out = "";
  const walk = (node: Node, inProse: boolean) => {
    if (node.nodeType === Node.TEXT_NODE) {
      if (inProse) out += node.textContent ?? "";
      return;
    }
    if (node instanceof Element) {
      if (node.matches(CHROME)) return;
      if (node.tagName === "BR") {
        out += "\n";
        return;
      }
    }
    const prose =
      inProse || (node instanceof Element && node.hasAttribute(PROSE_ATTR));
    node.childNodes.forEach((child) => walk(child, prose));
    if (!(node instanceof Element)) return;
    if (BLOCK.test(node.tagName)) out += "\n";
    else if (CELL.test(node.tagName)) out += " ";
  };
  walk(range.cloneContents(), withinOne);
  // Trailing blanks go and blank lines collapse; a line's indentation stays.
  return out.replace(/[ \t]+\n/g, "\n").replace(/\n{3,}/g, "\n\n");
}

/**
 * selectionTarget reads the passage a selection offers as an idea: both ends
 * in the prose of ONE assistant message, neither in a field, at least
 * MIN_SELECTION characters once trimmed. Null for anything else.
 */
export function selectionTarget(
  selection: Selection | null,
): SelectionTarget | null {
  const located = locate(selection);
  if (located === null) return null;
  const { range, start, messageId } = located;
  const withinOne = start.contains(range.commonAncestorContainer);
  const text = passageText(range, withinOne).trim();
  if (Array.from(text).length < MIN_SELECTION) return null;
  return { messageId, text };
}
