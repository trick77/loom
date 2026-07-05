// Pasting a very large block of text into the composer collapses it into a
// removable "Pasted" chip instead of flooding the textarea inline (mirrors
// claude.ai). The block's text is folded back into the outgoing message content
// on send — it is never uploaded, indexed, or counted against attachment limits.

// Character-count threshold above which a paste becomes a chip. Measured against
// claude.ai (its cutoff is ~4091); the trigger is character count only, never
// line count. A paste of this length or shorter is inserted inline as usual.
export const PASTE_AS_ATTACHMENT_THRESHOLD = 4000;

export type PastedText = {
  id: string;
  text: string;
  lineCount: number;
};

export function createPastedText(text: string): PastedText {
  return {
    // Avoid crypto.randomUUID: it is undefined in insecure contexts (plain
    // http://), which a corporate intranet deployment may well be. Date.now() +
    // random is unique enough for a transient composer-local id (same idiom as
    // the optimistic message ids in ThreadShell).
    id: `pasted-${Date.now()}-${Math.random().toString(36).slice(2)}`,
    text,
    lineCount: text.split("\n").length,
  };
}
