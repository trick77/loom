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

const SOURCES = [
  web(1, "reddit.com"),
  web(2, "kubernetes.io"),
  web(3, "truefoundry.com"),
];

describe("assignDisplayNumbers", () => {
  it("numbers sources by first citation, not by discovery order", () => {
    const content = "Scheduler changed [3]. The API shifted [2].";

    const { display, ordered } = assignDisplayNumbers(content, SOURCES);

    expect(display.get(3)).toBe(1);
    expect(display.get(2)).toBe(2);
    expect(ordered.map((c) => c.filename)).toEqual([
      "truefoundry.com",
      "kubernetes.io",
    ]);
  });

  it("excludes gathered sources the answer never cites", () => {
    const { ordered, display } = assignDisplayNumbers(
      "Only this one [2].",
      SOURCES,
    );

    expect(ordered).toHaveLength(1);
    expect(ordered[0].filename).toBe("kubernetes.io");
    expect(display.has(1)).toBe(false);
    expect(display.has(3)).toBe(false);
  });

  it("repeated markers keep their first-assigned number", () => {
    const { display, ordered } = assignDisplayNumbers(
      "One [3]. Two [2]. Again [3].",
      SOURCES,
    );

    expect(display.get(3)).toBe(1);
    expect(display.get(2)).toBe(2);
    expect(ordered).toHaveLength(2);
  });

  // Half of all answers with web sources cite nothing. Hiding uncited sources
  // would empty the Sources row entirely on those, losing all visibility into what
  // was searched.
  it("falls back to every gathered source when nothing is cited", () => {
    const { ordered, display } = assignDisplayNumbers(
      "No markers here.",
      SOURCES,
    );

    expect(ordered.map((c) => c.index)).toEqual([1, 2, 3]);
    expect(display.size).toBe(0);
  });

  it("ignores markers inside fenced and inline code", () => {
    const content = [
      "Prose with no citation.",
      "```js",
      "const x = arr[3];",
      "```",
      "Inline `argv[2]` too.",
    ].join("\n");

    const { ordered } = assignDisplayNumbers(content, SOURCES);

    // Neither code marker counts as a citation, so this is the uncited fallback.
    expect(ordered.map((c) => c.index)).toEqual([1, 2, 3]);
  });

  it("an unclosed fence still suppresses its markers mid-stream", () => {
    const { ordered } = assignDisplayNumbers(
      "text\n```js\nconst x = arr[3];",
      SOURCES,
    );

    expect(ordered.map((c) => c.index)).toEqual([1, 2, 3]);
  });

  it("unknown indices consume no display number", () => {
    const { display, ordered } = assignDisplayNumbers(
      "Bogus [9]. Real [2].",
      SOURCES,
    );

    expect(display.get(2)).toBe(1);
    expect(display.has(9)).toBe(false);
    expect(ordered).toHaveLength(1);
  });

  // The property that makes this safe to run against a partial answer: growing the
  // text can only append, never renumber what is already on screen.
  it("is append-only as the streamed text grows", () => {
    const full = "First [3]. Then [1]. Finally [2].";
    let previous = new Map<number, number>();

    for (let i = 1; i <= full.length; i++) {
      const { display } = assignDisplayNumbers(full.slice(0, i), SOURCES);
      for (const [index, number] of previous) {
        // Skip the uncited-fallback frames, which legitimately carry no mapping.
        if (display.size === 0) continue;
        expect(display.get(index)).toBe(number);
      }
      if (display.size > 0) previous = display;
    }

    expect(previous.get(3)).toBe(1);
    expect(previous.get(1)).toBe(2);
    expect(previous.get(2)).toBe(3);
  });

  it("drops uncited sources", () => {
    const { ordered } = assignDisplayNumbers("Only [3] so far.", SOURCES);

    expect(ordered.map((c) => c.index)).toEqual([3]);
  });

  // A document the user uploaded was genuinely consulted whether or not a sentence
  // cites it, so it stays in the row (unnumbered). An uncited *web* source is
  // dropped — showing it would imply an attribution the prose never made.
  it("keeps uncited documents but drops uncited web sources", () => {
    const doc: Citation = {
      documentId: "d1",
      filename: "guide.pdf",
      snippet: "x",
      score: 1,
      index: 1,
    };
    const cited = web(2, "kubernetes.io");
    const uncitedWeb = web(3, "reddit.com");

    const { ordered, display } = assignDisplayNumbers("Only [2] here.", [
      doc,
      cited,
      uncitedWeb,
    ]);

    expect(ordered.map((c) => c.index)).toEqual([2, 1]);
    expect(display.get(2)).toBe(1);
    expect(display.has(1)).toBe(false);
  });

  // Two uncited documents exercise the ordering: they trail the cited sources in
  // their own index order, not in whatever order the citations arrived.
  it("orders several uncited documents by index", () => {
    const docA: Citation = {
      documentId: "d3",
      filename: "c.md",
      snippet: "x",
      score: 1,
      index: 3,
    };
    const docB: Citation = {
      documentId: "d1",
      filename: "a.md",
      snippet: "x",
      score: 1,
      index: 1,
    };

    const { ordered } = assignDisplayNumbers("Only [2] cited.", [
      docA,
      docB,
      web(2, "kubernetes.io"),
    ]);

    expect(ordered.map((c) => c.index)).toEqual([2, 1, 3]);
  });

  it("numbers a cited document like any other source", () => {
    const doc: Citation = {
      documentId: "d1",
      filename: "guide.pdf",
      snippet: "x",
      score: 1,
      index: 1,
    };
    const { display, ordered } = assignDisplayNumbers("Per the guide [1].", [
      doc,
      web(2, "kubernetes.io"),
    ]);

    expect(display.get(1)).toBe(1);
    expect(ordered[0].filename).toBe("guide.pdf");
  });

  it("returns an empty numbering when there are no citations", () => {
    expect(assignDisplayNumbers("Text [1].", []).ordered).toEqual([]);
    expect(assignDisplayNumbers("Text [1].", undefined).ordered).toEqual([]);
  });

  // Documents from before they were numbered carry no index and cannot be cited.
  it("ignores citations without a usable index (legacy document chunks)", () => {
    const doc: Citation = {
      documentId: "d1",
      filename: "guide.pdf",
      snippet: "x",
      score: 1,
    };

    expect(assignDisplayNumbers("Cited [1].", [doc]).ordered).toEqual([]);
  });
});
