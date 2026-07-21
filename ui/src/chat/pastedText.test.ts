import { describe, expect, it } from "vitest";

import { stripPastedBlocks, type PastedTextBlock } from "./pastedText";

// Mirror composeSendContent (ThreadShell): draft first, then each block, joined by
// "\n\n", empties filtered. The store trims the whole string on insert.
function fold(draft: string, blocks: PastedTextBlock[]): string {
  return [draft.trim(), ...blocks.map((b) => b.text)]
    .filter((p) => p !== "")
    .join("\n\n")
    .trim();
}

const block = (text: string): PastedTextBlock => ({
  text,
  lineCount: text.split("\n").length,
});

describe("stripPastedBlocks", () => {
  it("returns content unchanged when there are no blocks", () => {
    expect(stripPastedBlocks("hello world", [])).toEqual({
      text: "hello world",
      matched: [],
    });
  });

  it("strips a single block, leaving the typed draft", () => {
    const b = block("a very\nlong pasted\nblock of text");
    const out = stripPastedBlocks(fold("what is this?", [b]), [b]);
    expect(out.text).toBe("what is this?");
    expect(out.matched).toEqual([true]);
  });

  it("leaves an empty string when the draft was empty (chip only)", () => {
    const b = block("only a paste\nno typed draft");
    const out = stripPastedBlocks(fold("", [b]), [b]);
    expect(out.text).toBe("");
    expect(out.matched).toEqual([true]);
  });

  it("strips multiple blocks in order", () => {
    const b1 = block("first block\nof pasted text");
    const b2 = block("second block\nof pasted text");
    const out = stripPastedBlocks(fold("draft", [b1, b2]), [b1, b2]);
    expect(out.text).toBe("draft");
    expect(out.matched).toEqual([true, true]);
  });

  it("tolerates trailing whitespace trimmed from the last block by the store", () => {
    const b = block("paste ending in blank lines\n\n\n");
    // fold() trims the whole content, so the stored form loses the block's trailing
    // newlines — the exact match fails and the trimmed retry must still strip it.
    const out = stripPastedBlocks(fold("hi", [b]), [b]);
    expect(out.text).toBe("hi");
    expect(out.matched).toEqual([true]);
  });

  it("keeps draft text that merely resembles the block", () => {
    const b = block("the pasted block content\nwith several lines here");
    const out = stripPastedBlocks(fold("short note", [b]), [b]);
    expect(out.text).toBe("short note");
    expect(out.matched).toEqual([true]);
  });

  it("reports a block that cannot be located as unmatched (so it is not double-rendered)", () => {
    const present = block("this block is in the content");
    const absent = block("this block was never folded in");
    const content = fold("note", [present]);
    const out = stripPastedBlocks(content, [present, absent]);
    // present is stripped and matched; absent is not in content, so unmatched and
    // left for the caller to keep inline rather than render as a duplicate chip.
    expect(out.matched).toEqual([true, false]);
  });
});
