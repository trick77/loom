// Pasting a very large block of text into the composer collapses it into a
// removable "Pasted" chip instead of flooding the textarea inline (mirrors
// claude.ai). The block's text is folded back into the outgoing message content
// on send — it is never uploaded, indexed, or counted against attachment limits.

// A paste collapses into a chip when it exceeds EITHER threshold — a wall of
// characters or a tall block of lines. claude.ai keys off character count only
// (its cutoff is ~4091); we collapse sooner and also on line count to keep the
// composer uncluttered. A paste under both thresholds is inserted inline as usual.
export const PASTE_AS_ATTACHMENT_THRESHOLD = 2000;
// Collapse only a genuine wall of lines: set well above a typical typed multi-line
// message or a short pasted email so it isn't triggered eagerly (the composer
// scrolls past ~11 lines, so these still scroll a little before collapsing).
export const PASTE_AS_ATTACHMENT_LINE_THRESHOLD = 25;

export type PastedText = {
  id: string;
  text: string;
  lineCount: number;
};

// PastedTextBlock is the persisted, wire shape of a collapsed paste (no client-only
// id): what the composer sends and the backend stores/returns on a message so the
// sent bubble can render a "Pasted" chip instead of the inline wall of text.
export type PastedTextBlock = {
  text: string;
  lineCount: number;
};

export function toPastedTextBlock(pasted: PastedText): PastedTextBlock {
  return { text: pasted.text, lineCount: pasted.lineCount };
}

export function pastedTextFromBlock(block: PastedTextBlock): PastedText {
  return { ...createPastedText(block.text), lineCount: block.lineCount };
}

// StripPastedResult carries the bubble text (blocks removed) plus, per input block,
// whether that block was actually found and removed from the content. A block that
// was NOT matched is still present inline in `text`, so callers must not also render
// it as a chip — otherwise the same paste would show both inline and as a chip.
export type StripPastedResult = {
  text: string;
  matched: boolean[];
};

// stripPastedBlocks removes the collapsed paste blocks from a message's stored
// content, returning the text the bubble should render (the typed draft alone; the
// matched blocks render as chips). It is the inverse of composeSendContent's fold,
// which appends each block after the draft joined by "\n\n".
//
// Blocks are peeled from the end (they are always trailing). The backend trims the
// whole content on insert, which can strip whitespace at the very first/last block
// edge, so a block that fails an exact match is retried trimmed before giving up. A
// block that still cannot be located is reported as unmatched (matched[i] === false)
// and left inline, so the caller renders it exactly once (inline, not also a chip).
export function stripPastedBlocks(
  content: string,
  blocks: PastedTextBlock[],
): StripPastedResult {
  const matched = new Array<boolean>(blocks.length).fill(false);
  let end = content.length;
  for (let i = blocks.length - 1; i >= 0; i--) {
    const hay = content.slice(0, end);
    let index = hay.lastIndexOf(blocks[i].text);
    if (index === -1) {
      index = hay.lastIndexOf(blocks[i].text.trim());
      if (index === -1) continue;
    }
    matched[i] = true;
    end = index;
    if (content.slice(0, end).endsWith("\n\n")) end -= 2;
  }
  return { text: content.slice(0, end), matched };
}

// Count lines without materializing an array of every line — a paste can be
// multiple megabytes and this only feeds a threshold check / aria-label.
export function countLines(text: string): number {
  let lineCount = 1;
  for (
    let index = text.indexOf("\n");
    index !== -1;
    index = text.indexOf("\n", index + 1)
  ) {
    lineCount += 1;
  }
  return lineCount;
}

// Whether a plain-text paste should collapse into a chip rather than insert inline.
export function shouldCollapsePaste(text: string): boolean {
  return (
    text.length > PASTE_AS_ATTACHMENT_THRESHOLD ||
    countLines(text) > PASTE_AS_ATTACHMENT_LINE_THRESHOLD
  );
}

export function createPastedText(text: string): PastedText {
  return {
    // Avoid crypto.randomUUID: it is undefined in insecure contexts (plain
    // http://), which a corporate intranet deployment may well be. Date.now() +
    // random is unique enough for a transient composer-local id (same idiom as
    // the optimistic message ids in ThreadShell).
    id: `pasted-${Date.now()}-${Math.random().toString(36).slice(2)}`,
    text,
    lineCount: countLines(text),
  };
}
