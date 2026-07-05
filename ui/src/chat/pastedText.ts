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

// Count lines without materializing an array of every line — a paste can be
// multiple megabytes and this only feeds a threshold check / aria-label.
export function countLines(text: string): number {
  let lineCount = 1;
  for (let index = text.indexOf("\n"); index !== -1; index = text.indexOf("\n", index + 1)) {
    lineCount += 1;
  }
  return lineCount;
}

// Whether a plain-text paste should collapse into a chip rather than insert inline.
export function shouldCollapsePaste(text: string): boolean {
  return text.length > PASTE_AS_ATTACHMENT_THRESHOLD || countLines(text) > PASTE_AS_ATTACHMENT_LINE_THRESHOLD;
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
