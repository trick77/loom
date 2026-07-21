import { describe, expect, it } from "vitest";

import { normalizeMathDelimiters } from "./mathDelimiters";

describe("normalizeMathDelimiters", () => {
  it("converts inline \\(...\\) to $$...$$", () => {
    expect(normalizeMathDelimiters("the value \\(x^2\\) grows")).toBe("the value $$x^2$$ grows");
  });

  it("converts display \\[...\\] to $$...$$", () => {
    expect(normalizeMathDelimiters("\\[g(x) = \\frac{a}{b}\\]")).toBe("$$g(x) = \\frac{a}{b}$$");
  });

  it("converts a multi-line display block", () => {
    expect(normalizeMathDelimiters("\\[\n a + b \n= c\n\\]")).toBe("$$\n a + b \n= c\n$$");
  });

  it("handles multiple math spans in one string", () => {
    expect(normalizeMathDelimiters("\\(a\\) then \\(b\\)")).toBe("$$a$$ then $$b$$");
  });

  it("leaves currency amounts in prose untouched", () => {
    const src = "the most bearish ($63) and most bullish ($800) targets, at ~$1.77T";
    expect(normalizeMathDelimiters(src)).toBe(src);
  });

  it("leaves existing $ and $$ delimiters untouched", () => {
    expect(normalizeMathDelimiters("inline $x$ and block $$y$$")).toBe("inline $x$ and block $$y$$");
  });

  it("leaves text without any math untouched", () => {
    expect(normalizeMathDelimiters("just prose, no math here")).toBe("just prose, no math here");
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
    expect(normalizeMathDelimiters(src)).toBe("Formula $$a$$:\n\n```\n\\[ literal \\]\n```");
  });

  it("preserves a tilde-fenced block", () => {
    const src = "~~~\n\\(keep\\)\n~~~";
    expect(normalizeMathDelimiters(src)).toBe(src);
  });

  it("handles a multi-backtick inline span containing a backtick", () => {
    const src = "``code with ` tick and \\(x\\)`` then \\(y\\)";
    expect(normalizeMathDelimiters(src)).toBe("``code with ` tick and \\(x\\)`` then $$y$$");
  });
});
