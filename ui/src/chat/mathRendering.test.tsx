import "@testing-library/jest-dom/vitest";
import { render } from "@testing-library/react";
import { expect, test } from "vitest";

import { ProseMarkdown } from "./messages";

// End-to-end proof of the render pipeline: remark-math + rehype-katex must turn LaTeX
// into KaTeX DOM (a `.katex` element), not leave the raw `$$`/`\(` delimiters as text.

test("renders a $$...$$ formula as KaTeX", () => {
  const { container } = render(
    <ProseMarkdown>{"$$g(x) = \\frac{x^2 - 4}{x^2 - x - 2}$$"}</ProseMarkdown>,
  );
  expect(container.querySelector(".katex")).not.toBeNull();
  expect(container.textContent).not.toContain("$$");
});

// The whole single-dollar-off approach rests on this: a `$$` run mid-line must stay
// *inline*, and only become a display block when it starts its own line. If that ever
// flips, inline math would break out into its own centred block.
test("renders mid-line $$...$$ as inline, not display, KaTeX", () => {
  const { container } = render(<ProseMarkdown>{"the value $$x^2$$ grows"}</ProseMarkdown>);
  expect(container.querySelector(".katex")).not.toBeNull();
  expect(container.querySelector(".katex-display")).toBeNull();
});

// Display math needs a real `$$` fence — the delimiters on their own lines. A one-line
// `$$x^2$$` is inline even at the start of a line, because micromark reads the rest of
// that line as the fence's meta string and falls back to math-text when it finds a `$`.
test("renders a $$ fence as display KaTeX", () => {
  const { container } = render(<ProseMarkdown>{"$$\nx^2\n$$"}</ProseMarkdown>);
  expect(container.querySelector(".katex-display")).not.toBeNull();
});

// Regression: `\[...\]` normalised to a one-line `$$ x $$`, which micromark reads as
// inline math, so LaTeX-native display math never produced a centred block.
test("renders \\[...\\] on its own line as display KaTeX", () => {
  const { container } = render(<ProseMarkdown>{"\\[g(x) = \\frac{a}{b}\\]"}</ProseMarkdown>);
  expect(container.querySelector(".katex-display")).not.toBeNull();
  expect(container.textContent).not.toContain("$$");
});

test("renders \\[...\\] mid-sentence as inline KaTeX", () => {
  const { container } = render(<ProseMarkdown>{"so \\[a + b\\] follows"}</ProseMarkdown>);
  expect(container.querySelector(".katex")).not.toBeNull();
  expect(container.querySelector(".katex-display")).toBeNull();
});

// Regression: an amount touching the end of a formula merged into the delimiter run, so
// neither the math nor the amount survived — the whole thing showed as raw `$$x$$$5`.
test("renders math abutting a currency amount", () => {
  const { container } = render(<ProseMarkdown>{"cost \\(x\\)$5 total"}</ProseMarkdown>);
  expect(container.querySelector(".katex")).not.toBeNull();
  expect(container.textContent).toContain("$5 total");
  expect(container.textContent).not.toContain("$$");
});

// Regression: prose about money used to pair into an inline math span, which swallowed
// the sentence between the two amounts and re-rendered it as italic KaTeX.
test.each([
  ['~$91, arguing the IPO price "is priced for 2032, not 2026" at $1.77T market cap', ["$91", "$1.77T"]],
  [
    "The spread between the most bearish ($63) and most bullish ($800) targets is wider than most companies' entire market caps.",
    ["$63", "$800"],
  ],
  [
    "Current price: ~$119.85 (last close), which is actually *below* the $135 IPO price from June 12.",
    ["$119.85", "$135", "below"],
  ],
] as const)("leaves currency amounts in prose as plain text: %s", (prose, expected) => {
  const { container } = render(<ProseMarkdown>{prose}</ProseMarkdown>);
  expect(container.querySelector(".katex")).toBeNull();
  for (const fragment of expected) {
    expect(container.textContent).toContain(fragment);
  }
});

test("renders legacy \\(...\\) delimiters as KaTeX", () => {
  const { container } = render(<ProseMarkdown>{"the value \\(x^2\\) grows"}</ProseMarkdown>);
  expect(container.querySelector(".katex")).not.toBeNull();
  expect(container.textContent).not.toContain("\\(");
});

test("leaves LaTeX inside a code fence untouched", () => {
  const { container } = render(<ProseMarkdown>{"```\n\\[ a \\]\n```"}</ProseMarkdown>);
  expect(container.querySelector(".katex")).toBeNull();
  expect(container.querySelector("code")?.textContent).toContain("\\[ a \\]");
});
