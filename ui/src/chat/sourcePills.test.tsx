import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { Citation } from "../api";
import { ProseMarkdown } from "./messages";
import { SourcesOpenerProvider, webSourceMap } from "./sourcePills";
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
    // The marker shows a bare number now — the plate delimits it, so the brackets
    // are gone — and the site name moved to the accessible label.
    expect(first).toHaveTextContent("1");
    expect(screen.getByRole("link", { name: "Modal" })).toHaveTextContent("2");
  });

  it("numbers by citation order, not by the persisted index", () => {
    // Modal is [2] to the model but is cited first, so the reader sees 1.
    renderProse("Modal first [2]. Truefoundry second [1].", citations);

    expect(screen.getByRole("link", { name: "Modal" })).toHaveTextContent("1");
    expect(screen.getByRole("link", { name: "Truefoundry" })).toHaveTextContent(
      "2",
    );
  });

  // The model writes "deploys models [1]", and only "[1]" is replaced — without
  // trimming, the space in front survives and the marker floats away from the word
  // it backs.
  it("trims the space between a word and its marker", () => {
    const { container } = renderProse(
      "TrueFoundry deploys models [1].",
      citations,
    );

    expect(container.textContent).toContain("models1");
  });

  // A citation's destination is the source list, not a new tab: the drawer holds
  // every source with its title and snippet, and losing the answer to a page load
  // is the wrong trade for one click.
  it("opens the sources drawer instead of navigating, when one exists", () => {
    const onOpen = vi.fn();
    render(
      <SourcesOpenerProvider onOpen={onOpen}>
        <ProseMarkdown
          sources={webSourceMap(citations)}
          display={
            assignDisplayNumbers("Modal runs Python [2].", citations).display
          }
        >
          {"Modal runs Python [2]."}
        </ProseMarkdown>
      </SourcesOpenerProvider>,
    );

    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Modal" }));

    // The display number is handed over so the drawer can scroll to that source.
    expect(onOpen).toHaveBeenCalledWith(1);
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

  // Adjacent multi-digit markers used to need brackets so "[12][13]" could not read
  // as "1213". The numerals are bare again, but each sits on its own plate, so the
  // delimiting is visual: what the DOM must guarantee is two separate elements, each
  // carrying its own whole number.
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

    const twelve = screen.getByRole("link", { name: "Site12" });
    const thirteen = screen.getByRole("link", { name: "Site13" });
    expect(twelve).toHaveTextContent(/^12$/);
    expect(thirteen).toHaveTextContent(/^13$/);
    // Two elements, not one run of text: nothing in the DOM can read as "1213".
    expect(twelve).not.toBe(thirteen);
    expect(container.querySelectorAll(".ui-source-pill")).toHaveLength(13);
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
    expect(screen.getByRole("link", { name: "Site5" })).toHaveTextContent("5");
  });
});
