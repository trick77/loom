import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { cleanResultText, highlightTerms, renderSnippet } from "./highlight";

describe("cleanResultText", () => {
  test("strips markdown emphasis, code, and strikethrough markers", () => {
    expect(cleanResultText("a **bold** and *italic* and `code` and ~~gone~~")).toBe(
      "a bold and italic and code and gone",
    );
  });

  test("strips heading hashes and blockquote markers at a segment start", () => {
    expect(cleanResultText("# Heading")).toBe("Heading");
    expect(cleanResultText("intro > quoted")).toBe("intro quoted");
  });

  test("strips unordered-list bullets but keeps hyphenated words", () => {
    expect(cleanResultText("- first item")).toBe("first item");
    expect(cleanResultText("a well-known value")).toBe("a well-known value");
  });

  test("keeps link text, drops the URL", () => {
    expect(cleanResultText("see [the docs](https://example.com/x) now")).toBe(
      "see the docs now",
    );
  });

  test("removes emoji, skin tones, and ZWJ sequences", () => {
    expect(cleanResultText("💬 Any alternatives")).toBe("Any alternatives");
    expect(cleanResultText("ok 👍🏽 done")).toBe("ok done");
    expect(cleanResultText("team 👨‍👩‍👧 here")).toBe("team here");
  });

  test("leaves snake_case, digits, and technical punctuation intact", () => {
    expect(cleanResultText("the backend_start column at 02:24:50 on 2026-04-03")).toBe(
      "the backend_start column at 02:24:50 on 2026-04-03",
    );
  });
});

describe("renderSnippet", () => {
  test("bolds the « » match and strips surrounding markdown noise", () => {
    const { container } = render(<>{renderSnippet("…use **«VPC»** endpoints…")}</>);
    const strong = container.querySelector("strong");
    expect(strong).toHaveTextContent("VPC");
    // The ** markers around the match are gone.
    expect(container.textContent).toBe("…use VPC endpoints…");
  });
});

describe("highlightTerms", () => {
  test("falls back to the raw title when cleaning empties it", () => {
    render(<div data-testid="t">{highlightTerms("💬", "x")}</div>);
    expect(screen.getByTestId("t")).toHaveTextContent("💬");
  });
});
