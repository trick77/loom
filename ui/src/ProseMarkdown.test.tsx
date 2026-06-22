import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { ProseMarkdown } from "./ThreadShell";

const codeSample = "```ts\nconst answer = 42;\n```";

test("applies syntax highlighting to fenced code blocks", () => {
  const { container } = render(<ProseMarkdown>{codeSample}</ProseMarkdown>);

  // rehype-highlight adds the .hljs class and token spans
  expect(container.querySelector("code.hljs")).not.toBeNull();
  expect(container.querySelector(".hljs-keyword")).not.toBeNull();
});

test("renders a copy button for each code block", () => {
  render(<ProseMarkdown>{codeSample}</ProseMarkdown>);
  expect(screen.getByRole("button", { name: "Code kopieren" })).toBeInTheDocument();
});

test("copies the code text to the clipboard and shows feedback", async () => {
  const writeText = vi.fn();
  vi.stubGlobal("navigator", { clipboard: { writeText } });

  render(<ProseMarkdown>{codeSample}</ProseMarkdown>);
  fireEvent.click(screen.getByRole("button", { name: "Code kopieren" }));

  expect(writeText).toHaveBeenCalledWith("const answer = 42;\n");
  await waitFor(() => expect(screen.getByRole("button", { name: "Kopiert" })).toBeInTheDocument());

  vi.unstubAllGlobals();
});
