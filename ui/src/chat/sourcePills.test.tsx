import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Citation } from "../api";
import { ProseMarkdown } from "./messages";
import { webSourceMap } from "./sourcePills";
import { assignDisplayNumbers } from "./sourceNumbering";

// The pill plugin needs both the [n] -> source map and the persisted-index ->
// display-number map; in the app both come off the same citation list.
function renderProse(content: string, citations: Citation[]) {
  return render(
    <ProseMarkdown
      sources={webSourceMap(citations)}
      display={assignDisplayNumbers(content, citations).display}
    >
      {content}
    </ProseMarkdown>,
  );
}

describe("webSourceMap", () => {
  it("collects web citations (url + index) and ignores RAG doc citations", () => {
    const citations: Citation[] = [
      { documentId: "d1", filename: "guide.pdf", snippet: "x", score: 0.9 },
      {
        documentId: "",
        filename: "Truefoundry",
        snippet: "",
        score: 0,
        url: "https://truefoundry.com",
        index: 1,
      },
      {
        documentId: "",
        filename: "Modal",
        snippet: "",
        score: 0,
        url: "https://modal.com",
        index: 2,
      },
    ];
    const map = webSourceMap(citations);
    expect(map?.size).toBe(2);
    expect(map?.get(1)).toEqual({
      url: "https://truefoundry.com",
      label: "Truefoundry",
    });
    expect(map?.get(2)?.label).toBe("Modal");
  });

  it("returns undefined when there are no web citations", () => {
    expect(
      webSourceMap([
        { documentId: "d1", filename: "a.pdf", snippet: "", score: 0.5 },
      ]),
    ).toBeUndefined();
    expect(webSourceMap([])).toBeUndefined();
    expect(webSourceMap(undefined)).toBeUndefined();
  });
});

describe("ProseMarkdown inline source pills", () => {
  const citations: Citation[] = [
    {
      documentId: "",
      filename: "Truefoundry",
      snippet: "",
      score: 0,
      url: "https://truefoundry.com",
      index: 1,
    },
    {
      documentId: "",
      filename: "Modal",
      snippet: "",
      score: 0,
      url: "https://modal.com",
      index: 2,
    },
  ];

  it("renders the display number, linked, with the site name as its label", () => {
    renderProse(
      "TrueFoundry deploys models [1]. Modal runs Python [2].",
      citations,
    );

    const first = screen.getByRole("link", { name: "Truefoundry" });
    expect(first).toHaveAttribute("href", "https://truefoundry.com");
    expect(first).toHaveAttribute("target", "_blank");
    // The marker shows a number now; the site name moved to the accessible label.
    expect(first).toHaveTextContent("[1]");
    expect(screen.getByRole("link", { name: "Modal" })).toHaveTextContent(
      "[2]",
    );
  });

  it("numbers by citation order, not by the persisted index", () => {
    // Modal is [2] to the model but is cited first, so the reader sees 1.
    renderProse("Modal first [2]. Truefoundry second [1].", citations);

    expect(screen.getByRole("link", { name: "Modal" })).toHaveTextContent(
      "[1]",
    );
    expect(screen.getByRole("link", { name: "Truefoundry" })).toHaveTextContent(
      "[2]",
    );
  });

  it("leaves unknown markers as literal text", () => {
    renderProse("An out-of-range marker [7] stays as text.", citations);

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText(/\[7\]/)).toBeInTheDocument();
  });

  it("does not create pills when no source map is provided", () => {
    render(<ProseMarkdown>{"Plain answer with a [1] marker."}</ProseMarkdown>);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText(/\[1\]/)).toBeInTheDocument();
  });

  // Brackets delimit adjacent markers themselves — "[12][13]" cannot read as one
  // number the way bare superscripts "1213" could, so no separator is needed.
  it("keeps adjacent multi-digit markers legible", () => {
    const many: Citation[] = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13].map(
      (index) => ({
        documentId: "",
        filename: `Site${index}`,
        snippet: "",
        score: 0,
        url: `https://site${index}.com`,
        index,
      }),
    );
    const lead = Array.from(
      { length: 11 },
      (_, i) => `Claim ${i}. [${i + 1}]`,
    ).join(" ");

    const { container } = renderProse(`${lead} Finally [12][13].`, many);

    expect(container.textContent).toContain("[12][13]");
    expect(container.textContent).not.toContain("1213");
    expect(screen.getByRole("link", { name: "Site12" })).toHaveTextContent(
      "[12]",
    );
    expect(screen.getByRole("link", { name: "Site13" })).toHaveTextContent(
      "[13]",
    );
  });

  // Adjacency was tracked across text nodes, but markdown splits "[1]**bold**[2]"
  // into three of them. The second marker then looked like it abutted the first,
  // and a same-source repeat was silently deleted.
  it("keeps both markers when an element separates them", () => {
    renderProse("Claim.[1]**bold**[2] tail.", citations);

    expect(screen.getAllByRole("link")).toHaveLength(2);
  });

  it("keeps a repeat of one source separated by an element", () => {
    renderProse("Claim.[1]**bold**[1] tail.", citations);

    expect(screen.getAllByRole("link", { name: "Truefoundry" })).toHaveLength(
      2,
    );
  });

  it("keeps a repeat across paragraphs", () => {
    renderProse("First para ends.[1]\n\n[1] Second para.", citations);

    expect(screen.getAllByRole("link", { name: "Truefoundry" })).toHaveLength(
      2,
    );
  });

  it("keeps a repeat across list items", () => {
    renderProse("- Point one.[1]\n- [1] Point two.", citations);

    expect(screen.getAllByRole("link", { name: "Truefoundry" })).toHaveLength(
      2,
    );
  });

  it("collapses an immediately repeated same-source marker", () => {
    renderProse("Repeated cite [1][1] here.", citations);

    expect(screen.getAllByRole("link", { name: "Truefoundry" })).toHaveLength(
      1,
    );
  });

  it("keeps a repeat that backs a separate claim later in the answer", () => {
    renderProse("First claim [1]. A different claim [1].", citations);

    // Per-sentence attribution is the point, so a non-adjacent repeat stays.
    expect(screen.getAllByRole("link", { name: "Truefoundry" })).toHaveLength(
      2,
    );
  });

  // The old per-message cap of 3 existed because label pills were visually heavy.
  // Markers are numerals now, and measurement showed no end-clustering, so every
  // cited source renders — dropping them would be deleting attribution.
  it("renders every cited source, with no cap", () => {
    const many: Citation[] = [1, 2, 3, 4, 5].map((index) => ({
      documentId: "",
      filename: `Site${index}`,
      snippet: "",
      score: 0,
      url: `https://site${index}.com`,
      index,
    }));

    renderProse("A well-cited answer [1][2][3][4][5].", many);

    expect(screen.getAllByRole("link")).toHaveLength(5);
    expect(screen.getByRole("link", { name: "Site5" })).toHaveTextContent(
      "[5]",
    );
  });
});
