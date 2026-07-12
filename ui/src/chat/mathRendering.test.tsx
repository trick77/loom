import "@testing-library/jest-dom/vitest";
import { render } from "@testing-library/react";
import { expect, test } from "vitest";

import { ProseMarkdown } from "./messages";

// End-to-end proof of the render pipeline: remark-math + rehype-katex must turn LaTeX
// into KaTeX DOM (a `.katex` element), not leave the raw `$$`/`\(` delimiters as text.

test("renders $$...$$ display math as KaTeX", () => {
  const { container } = render(
    <ProseMarkdown>{"$$g(x) = \\frac{x^2 - 4}{x^2 - x - 2}$$"}</ProseMarkdown>,
  );
  expect(container.querySelector(".katex")).not.toBeNull();
  expect(container.textContent).not.toContain("$$");
});

test("renders inline $...$ math as KaTeX", () => {
  const { container } = render(<ProseMarkdown>{"the value $x^2$ grows"}</ProseMarkdown>);
  expect(container.querySelector(".katex")).not.toBeNull();
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
