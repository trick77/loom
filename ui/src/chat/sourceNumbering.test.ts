import { describe, expect, it } from "vitest";

import type { Citation } from "../api";
import { assignDisplayNumbers } from "./sourceNumbering";

function web(index: number, host: string): Citation {
  return {
    documentId: "",
    filename: host,
    snippet: "",
    score: 0,
    url: `https://${host}/page`,
    index,
  };
}

function doc(index: number, filename: string): Citation {
  return { documentId: `d${index}`, filename, snippet: "", score: 1, index };
}

const SOURCES = [
  web(1, "reddit.com"),
  web(2, "kubernetes.io"),
  web(3, "truefoundry.com"),
];

describe("assignDisplayNumbers", () => {
  it("numbers sources by first citation, not by discovery order", () => {
    const { display } = assignDisplayNumbers(
      "Scheduler changed [3]. The API shifted [2].",
      SOURCES,
    );

    expect(display.get(3)).toBe(1);
    expect(display.get(2)).toBe(2);
  });

  it("gives no number to a source the answer never cites", () => {
    const { display } = assignDisplayNumbers("Only this one [2].", SOURCES);

    expect(display.get(2)).toBe(1);
    expect(display.has(1)).toBe(false);
    expect(display.has(3)).toBe(false);
  });

  it("repeated markers keep their first-assigned number", () => {
    const { display } = assignDisplayNumbers(
      "One [3]. Two [2]. Again [3].",
      SOURCES,
    );

    expect(display.get(3)).toBe(1);
    expect(display.get(2)).toBe(2);
  });

  it("numbers a cited document like any other source", () => {
    const { display } = assignDisplayNumbers("Per the guide [1].", [
      doc(1, "guide.pdf"),
      web(2, "kubernetes.io"),
    ]);

    expect(display.get(1)).toBe(1);
  });

  it("unknown indices consume no display number", () => {
    const { display } = assignDisplayNumbers("Bogus [9]. Real [2].", SOURCES);

    expect(display.get(2)).toBe(1);
    expect(display.has(9)).toBe(false);
  });

  it("returns an empty numbering when there are no citations", () => {
    expect(assignDisplayNumbers("Text [1].", []).display.size).toBe(0);
    expect(assignDisplayNumbers("Text [1].", undefined).display.size).toBe(0);
  });

  // Documents persisted before they were numbered carry no index and cannot be
  // cited. They must still reach the Sources row — that is MessageCitations' job,
  // which is why this function no longer decides what is displayed.
  it("ignores citations without a usable index (legacy document chunks)", () => {
    const legacy: Citation = {
      documentId: "d1",
      filename: "guide.pdf",
      snippet: "x",
      score: 1,
    };

    expect(assignDisplayNumbers("Cited [1].", [legacy]).display.size).toBe(0);
  });

  // The property that makes this safe to run against a partial answer: growing the
  // text can only append, never renumber what is already on screen.
  it("is append-only as the streamed text grows", () => {
    const full = "First [3]. Then [1]. Finally [2].";
    let previous = new Map<number, number>();

    for (let i = 1; i <= full.length; i++) {
      const { display } = assignDisplayNumbers(full.slice(0, i), SOURCES);
      for (const [index, number] of previous) {
        expect(display.get(index)).toBe(number);
      }
      previous = display;
    }

    expect(previous.get(3)).toBe(1);
    expect(previous.get(1)).toBe(2);
    expect(previous.get(2)).toBe(3);
  });

  describe("code is not citation", () => {
    // A marker counted here but skipped by the pill plugin still consumes a display
    // number, so the genuinely cited source renders under the wrong one. The two
    // must agree on what counts as code.
    it("ignores markers inside fenced and inline code", () => {
      const content = [
        "Prose.",
        "```js",
        "const x = arr[3];",
        "```",
        "Inline `argv[2]` too.",
        "Real claim [1].",
      ].join("\n");

      const { display } = assignDisplayNumbers(content, SOURCES);

      expect(display.get(1)).toBe(1);
      expect(display.has(2)).toBe(false);
      expect(display.has(3)).toBe(false);
    });

    it("ignores markers in an indented code block", () => {
      const content = "Intro.\n\n    console.log(argv[1]);\n\nReal claim [2].";

      const { display } = assignDisplayNumbers(content, SOURCES);

      // [2] is the first *cited* source, so it must display as 1 — not 2, sitting
      // behind an array index that was never a citation.
      expect(display.get(2)).toBe(1);
      expect(display.has(1)).toBe(false);
    });

    it("ignores markers in a ~~~ fence", () => {
      const content = "Intro.\n\n~~~\nconst x = arr[1];\n~~~\n\nClaim [2].";

      const { display } = assignDisplayNumbers(content, SOURCES);

      expect(display.get(2)).toBe(1);
      expect(display.has(1)).toBe(false);
    });

    it("ignores markers in a double-backtick span", () => {
      const { display } = assignDisplayNumbers(
        "Use ``arr[1]`` here. Claim [2].",
        SOURCES,
      );

      expect(display.get(2)).toBe(1);
      expect(display.has(1)).toBe(false);
    });

    it("an unclosed fence still suppresses its markers mid-stream", () => {
      const { display } = assignDisplayNumbers(
        "text\n```js\nconst x = arr[3];",
        SOURCES,
      );

      expect(display.has(3)).toBe(false);
    });

    it("an unclosed inline span suppresses its marker mid-stream", () => {
      // Streaming "Facts [1]. Use `arr[2]" must not hand [2] a number it would then
      // lose once the closing backtick arrives.
      const { display } = assignDisplayNumbers(
        "Facts [1]. Use `arr[2]",
        SOURCES,
      );

      expect(display.get(1)).toBe(1);
      expect(display.has(2)).toBe(false);
    });
  });
});
