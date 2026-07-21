import { describe, expect, it } from "vitest";

import { normalizeMathDelimiters } from "./mathDelimiters";

describe("normalizeMathDelimiters", () => {
  it("converts inline \\(...\\) to $$...$$", () => {
    expect(normalizeMathDelimiters("the value \\(x^2\\) grows")).toBe(
      "the value $$x^2$$ grows",
    );
  });

  it("strips stray placeholder control characters from the input", () => {
    expect(normalizeMathDelimiters("a\u0001b and c\u0002d")).toBe("ab and cd");
  });

  it("does not fence an unclosed \\[ across a CRLF paragraph break", () => {
    const src = "\\[a\r\n\r\npara one\r\n\r\npara two \\[b\\]";
    expect(normalizeMathDelimiters(src)).not.toContain("$$\n");
  });

  it("does not fence a body whose blank line is hidden inside a code block", () => {
    const src = "\\[a\n```js\nx\n\ny\n```\nmore prose\nend\\]";
    expect(normalizeMathDelimiters(src)).not.toContain("$$\n");
  });

  it("escapes a bare dollar at the start of a formula body", () => {
    expect(normalizeMathDelimiters("cost \\($x\\) total")).toBe(
      "cost $$\\$x$$ total",
    );
  });

  it("parts a bare dollar at the end of a formula from the closing delimiter", () => {
    expect(normalizeMathDelimiters("cost \\(x$\\) total")).toBe(
      "cost $$x\\$ $$ total",
    );
  });

  it("leaves a dollar mid-body alone, since it cannot lengthen a delimiter run", () => {
    expect(normalizeMathDelimiters("\\(a$b\\)")).toBe("$$a$b$$");
  });

  it("leaves a tab-indented formula on the inline path", () => {
    expect(normalizeMathDelimiters("\t\\[a\\]")).toBe("\t$$a$$");
  });

  it("converts display \\[...\\] to a $$ fence", () => {
    expect(normalizeMathDelimiters("\\[g(x) = \\frac{a}{b}\\]")).toBe(
      "$$\ng(x) = \\frac{a}{b}\n$$",
    );
  });

  it("keeps the fence indented with its line, for a formula inside a list item", () => {
    expect(normalizeMathDelimiters("- item\n\n  \\[a + b\\]")).toBe(
      "- item\n\n  $$\n  a + b\n  $$",
    );
  });

  it("keeps two formulas on one line as two inline spans", () => {
    expect(normalizeMathDelimiters("\\[a\\] and \\[b\\]")).toBe(
      "$$a$$ and $$b$$",
    );
  });

  it("leaves a four-space-indented formula on the inline path, unchanged from before", () => {
    expect(normalizeMathDelimiters("    \\[a\\]")).toBe("    $$a$$");
  });

  // An unclosed `\[` still gets paired with a later `\]` by the inline branch, as it always
  // has. That stays harmless because inline `$$` cannot span a blank line, so it simply
  // fails to parse and the prose remains readable — see the render test. What must not
  // happen is the block branch claiming it: a `$$` fence *does* span blank lines, so it
  // would swallow the paragraphs into the formula and lose them.
  it("does not fence an unclosed \\[ that runs past a paragraph break", () => {
    const src = "\\[a\n\npara one\n\npara two \\[b\\]";
    expect(normalizeMathDelimiters(src)).not.toContain("$$\n");
  });

  it("does not double the indent when the closing delimiter is indented", () => {
    expect(normalizeMathDelimiters("  \\[\n  a\n  \\]")).toBe(
      "  $$\n  a\n  $$",
    );
  });

  it("leaves a formula mid-sentence inline", () => {
    expect(normalizeMathDelimiters("so \\[a + b\\] follows")).toBe(
      "so $$a + b$$ follows",
    );
  });

  it("fences a display formula that follows a line of prose", () => {
    expect(normalizeMathDelimiters("Formula:\n\\[a\\]")).toBe(
      "Formula:\n$$\na\n$$",
    );
  });

  it("converts a multi-line display block", () => {
    expect(normalizeMathDelimiters("\\[\n a + b \n= c\n\\]")).toBe(
      "$$\n a + b \n= c\n$$",
    );
  });

  it("handles multiple math spans in one string", () => {
    expect(normalizeMathDelimiters("\\(a\\) then \\(b\\)")).toBe(
      "$$a$$ then $$b$$",
    );
  });

  it("escapes an amount abutting the end of a formula", () => {
    expect(normalizeMathDelimiters("cost \\(x\\)$5 total")).toBe(
      "cost $$x$$\\$5 total",
    );
  });

  it("escapes an amount abutting the start of a formula", () => {
    expect(normalizeMathDelimiters("5$\\(x\\) total")).toBe("5\\$$$x$$ total");
  });

  it("leaves a dollar inside the formula body for KaTeX", () => {
    expect(normalizeMathDelimiters("\\(a \\$ b\\)")).toBe("$$a \\$ b$$");
  });

  it("does not escape an already-escaped dollar", () => {
    expect(normalizeMathDelimiters("cost \\$\\(x\\)")).toBe("cost \\$$$x$$");
  });

  it("leaves an amount separated by a space alone", () => {
    expect(normalizeMathDelimiters("\\(x\\) costs $5")).toBe("$$x$$ costs $5");
  });

  it("leaves currency amounts in prose untouched", () => {
    const src =
      "the most bearish ($63) and most bullish ($800) targets, at ~$1.77T";
    expect(normalizeMathDelimiters(src)).toBe(src);
  });

  it("leaves existing $ and $$ delimiters untouched", () => {
    expect(normalizeMathDelimiters("inline $x$ and block $$y$$")).toBe(
      "inline $x$ and block $$y$$",
    );
  });

  it("leaves text without any math untouched", () => {
    expect(normalizeMathDelimiters("just prose, no math here")).toBe(
      "just prose, no math here",
    );
  });

  it("does not touch \\( inside an inline code span", () => {
    const src = "use `\\(x\\)` for inline math";
    expect(normalizeMathDelimiters(src)).toBe(src);
  });

  it("does not touch \\[ inside a fenced code block", () => {
    const src = "```latex\n\\[ a \\]\n```";
    expect(normalizeMathDelimiters(src)).toBe(src);
  });

  it("converts prose math while preserving an adjacent code fence", () => {
    const src = "Formula \\(a\\):\n\n```\n\\[ literal \\]\n```";
    expect(normalizeMathDelimiters(src)).toBe(
      "Formula $$a$$:\n\n```\n\\[ literal \\]\n```",
    );
  });

  it("preserves a tilde-fenced block", () => {
    const src = "~~~\n\\(keep\\)\n~~~";
    expect(normalizeMathDelimiters(src)).toBe(src);
  });

  it("handles a multi-backtick inline span containing a backtick", () => {
    const src = "``code with ` tick and \\(x\\)`` then \\(y\\)";
    expect(normalizeMathDelimiters(src)).toBe(
      "``code with ` tick and \\(x\\)`` then $$y$$",
    );
  });
});
