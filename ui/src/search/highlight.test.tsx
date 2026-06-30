import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, test } from "vitest";

import { cleanResultText, highlightTerms, renderSnippet } from "./highlight";

describe("cleanResultText", () => {
  test("strips bold, italic, inline-code, and strikethrough markers", () => {
    expect(cleanResultText("a **bold** and *italic* and `code` and ~~gone~~")).toBe(
      "a bold and italic and code and gone",
    );
  });

  test("strips truncated/unpaired emphasis markers from snippet windows", () => {
    // FTS returns a cut-off window, so the closing ** is often out of view.
    expect(cleanResultText("providers still need **re")).toBe("providers still need re");
    expect(cleanResultText("…the **bold")).toBe("…the bold");
    expect(cleanResultText("run `npm tes")).toBe("run npm tes");
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

  // Regression: space-isolated operators and inline punctuation must pass through
  // unchanged — only markers that TOUCH text (markdown) are stripped.
  test("keeps space-isolated operators and inline punctuation", () => {
    for (const s of [
      "2 * 3",
      "2 ** 3 exponent",
      "a > b",
      "5 > 3 is true",
      "1 + 2 = 3",
      "rust - lang notes",
      "C++ guide",
      "__init__ method",
      "stuck ~30h now",
      "the backend_start column at 02:24:50 on 2026-04-03",
    ]) {
      expect(cleanResultText(s)).toBe(s);
    }
  });

  // Documented tradeoff: a marker touching text reads as markdown, so a leading-
  // star token loses its star in the preview. Acceptable for a snippet locator.
  test("leading-star tokens lose the star (known tradeoff)", () => {
    expect(cleanResultText("*.tsx files")).toBe(".tsx files");
    expect(cleanResultText("char *p")).toBe("char p");
    expect(cleanResultText("**kwargs")).toBe("kwargs");
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
    // The ** markers around the match are gone; a short lead (under budget) is kept.
    expect(container.textContent).toBe("…use VPC endpoints…");
  });

  test("trims a long centred lead to a bounded amount, keeping the match mid-line", () => {
    // FTS centres the match; an over-long lead would clip the highlight off the
    // truncated single-line subline, so the lead is capped (not dropped) — the
    // match still reads mid-sentence with context, like claude.ai.
    const centred =
      "…and only after all of that is in place do we finally configure the «ingress» controller for traffic…";
    const { container } = render(<>{renderSnippet(centred)}</>);
    const strong = container.querySelector("strong");
    expect(strong).toHaveTextContent("ingress");
    // Bounded leading context is retained (word-snapped); the match is no longer
    // jammed against the left edge.
    expect(container.textContent).toBe(
      "…do we finally configure the ingress controller for traffic…",
    );
  });

  test("leaves a match that already leads the snippet untouched", () => {
    const { container } = render(<>{renderSnippet("«VPS»; it provisions workspaces…")}</>);
    expect(container.textContent).toBe("VPS; it provisions workspaces…");
  });
});

describe("highlightTerms", () => {
  test("falls back to the raw title when cleaning empties it", () => {
    render(<div data-testid="t">{highlightTerms("💬", "x")}</div>);
    expect(screen.getByTestId("t")).toHaveTextContent("💬");
  });
});
