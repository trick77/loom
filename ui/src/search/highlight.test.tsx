import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { cleanResultText, highlightTerms, renderSnippet } from "./highlight";

describe("cleanResultText", () => {
  test("strips paired bold, inline-code, and strikethrough markers", () => {
    expect(cleanResultText("a **bold** and `code` and ~~gone~~")).toBe(
      "a bold and code and gone",
    );
  });

  test("strips heading and blockquote/list markers only at a line start", () => {
    expect(cleanResultText("# Heading")).toBe("Heading");
    expect(cleanResultText("intro\n> quoted")).toBe("intro quoted");
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
    expect(cleanResultText("check ✔️ mark")).toBe("check mark");
  });

  // Regression: the first implementation blanket-stripped every * ` > - + and
  // every \p{Extended_Pictographic}, mangling the code/math/symbol content that
  // dominates these conversations. These must pass through unchanged.
  test("does not corrupt code, globs, math, or operators", () => {
    for (const s of [
      "*.tsx files",
      "**kwargs",
      "char *p",
      "2 * 3",
      "2 ** 3 exponent",
      "a > b",
      "5 > 3 is true",
      "1 + 2 = 3",
      "rust - lang notes",
      "C++ guide",
      "__init__ method",
      "the backend_start column at 02:24:50 on 2026-04-03",
    ]) {
      expect(cleanResultText(s)).toBe(s);
    }
  });

  test("keeps text-presentation symbols (™ © ® ✔ →)", () => {
    expect(cleanResultText("Acme™ release")).toBe("Acme™ release");
    expect(cleanResultText("Foo © 2026")).toBe("Foo © 2026");
    expect(cleanResultText("✔ done plain")).toBe("✔ done plain");
    expect(cleanResultText("arrow → here")).toBe("arrow → here");
  });

  test("preserves « » FTS match markers even inside a link URL", () => {
    expect(cleanResultText("[label](http://«ex».com) x")).toBe("[label](http://«ex».com) x");
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
