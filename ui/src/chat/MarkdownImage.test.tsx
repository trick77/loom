import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

import { MemoryMarkdown } from "../MemoryMarkdown";
import { ActivityTracePanel } from "./ActivityTracePanel";
import { MarkdownImage } from "./MarkdownImage";
import { ProseMarkdown } from "./messages";

// A clicked image stays loaded for the session, so each test uses its own URL.
const evil = (test: string) => `https://evil.example/p.png?d=${test}`;

test("a remote image loads only once clicked, and stays loaded", () => {
  const src = evil("click");
  const first = render(<MarkdownImage src={src} alt="Poster" />);
  expect(first.container.querySelector("img")).toBeNull();
  fireEvent.click(
    screen.getByRole("button", {
      name: "Load image from evil.example: Poster",
    }),
  );
  expect(first.container.querySelector("img")).toHaveAttribute("src", src);
  first.unmount();
  // A re-rendered answer keeps the image the user chose to load.
  const again = render(<MarkdownImage src={src} alt="Poster" />);
  expect(again.container.querySelector("img")).toHaveAttribute("src", src);
});

test("artifact and inline images show at once, a bare filename not at all", () => {
  const data = "data:image/png;base64,iVBORw0KGgo=";
  const artifact = `${window.location.origin}/api/artifacts/a1/download`;
  expect(
    render(<MarkdownImage src={data} alt="" />).container.querySelector("img"),
  ).toHaveAttribute("src", data);
  expect(
    render(<MarkdownImage src={artifact} alt="" />).container.querySelector(
      "img",
    ),
  ).toHaveAttribute("src", artifact);
  expect(
    render(<MarkdownImage src="lego-set.png" alt="Lego" />).container
      .firstChild,
  ).toBeNull();
});

test("loom's own image proxies wait for a click: they fetch what they are given", () => {
  const origin = window.location.origin;
  for (const src of [
    `${origin}/api/source-image?u=${encodeURIComponent(evil("proxy"))}`,
    `${origin}/api/favicon?u=https://secret.evil.example/`,
    `${origin}/api/artifacts/a1/download?u=x`,
  ]) {
    const { container, unmount } = render(<MarkdownImage src={src} alt="" />);
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("button")).not.toBeNull();
    unmount();
  }
});

test("loading a linked image does not follow the link", () => {
  const { container } = render(
    <ProseMarkdown>{`[![badge](${evil("link")})](https://evil.example/track)`}</ProseMarkdown>,
  );
  // fireEvent returns false when the click's default (following) was prevented.
  const followed = fireEvent.click(
    screen.getByRole("button", { name: /Load image from evil\.example/ }),
  );
  expect(followed).toBe(false);
  expect(container.querySelector("img")).not.toBeNull();
});

test("the host stays apart from the alt text an image could fake it with", () => {
  render(<MarkdownImage src={evil("spoof")} alt="example.com (trusted)" />);
  expect(screen.getByText("Load image from evil.example")).toHaveClass(
    "ui-markdown-image-host",
  );
  expect(screen.getByText("example.com (trusted)")).toHaveClass(
    "ui-markdown-image-alt",
  );
});

test("no markdown surface loads a remote image unasked", () => {
  const src = evil("surfaces");
  const markdown = `Look ![Poster](${src})`;
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
    expect(container.querySelector(`img[src="${src}"]`)).toBeNull();
  }
  expect(
    screen.getAllByRole("button", {
      name: "Load image from evil.example: Poster",
    }),
  ).toHaveLength(3);
});
