import "@testing-library/jest-dom/vitest";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";
import type { ComponentProps } from "react";

import {
  combineLikeSources,
  dedupeByDomain,
  MessageCitations,
} from "./Citations";
import type { Citation } from "../api";

// The backend sets filename to the registrable-domain label (its main label via
// the public-suffix list, e.g. "Github"); dedupeByDomain keys on it. Callers may
// omit label to exercise the empty-label fallback.
const webSource = (url: string, index: number, label = ""): Citation => ({
  documentId: "",
  filename: label,
  snippet: "",
  score: 0,
  url,
  index,
});

describe("dedupeByDomain", () => {
  it("collapses subdomains of one site (same registrable-domain label) to one, preserving first-seen order", () => {
    const sources = [
      webSource("https://github.com/torvalds", 1, "Github"),
      webSource("https://docs.github.com/en/actions", 2, "Github"),
      webSource("https://gist.github.com/abc", 3, "Github"),
      webSource("https://modal.com/docs", 4, "Modal"),
    ];
    const deduped = dedupeByDomain(sources);
    expect(deduped.map((s) => s.url)).toEqual([
      "https://github.com/torvalds",
      "https://modal.com/docs",
    ]);
  });

  it("treats labels case-insensitively", () => {
    const deduped = dedupeByDomain([
      webSource("https://example.com/a", 1, "Example"),
      webSource("https://www.example.com/b", 2, "example"),
    ]);
    expect(deduped).toHaveLength(1);
  });

  it("keeps different registrable domains that share a first label separate", () => {
    // Two distinct sites whose backend labels differ are not merged.
    const deduped = dedupeByDomain([
      webSource("https://modal.com/a", 1, "Modal"),
      webSource("https://truefoundry.com/b", 2, "Truefoundry"),
    ]);
    expect(deduped).toHaveLength(2);
  });

  it("falls back to host/url and does not collapse sources with an empty label", () => {
    const deduped = dedupeByDomain([
      webSource("https://198.51.100.7/a", 1),
      webSource("https://203.0.113.9/b", 2),
      webSource("not a url", 3),
    ]);
    expect(deduped).toHaveLength(3);
  });
});

describe("combineLikeSources", () => {
  it("groups chunks by filename, counts references, and keeps the best snippet", () => {
    const sources: Citation[] = [
      { documentId: "d1", filename: "guide.pdf", snippet: "low", score: 0.2 },
      { documentId: "d1", filename: "guide.pdf", snippet: "high", score: 0.9 },
      { documentId: "d2", filename: "notes.md", snippet: "only", score: 0.5 },
    ];
    const combined = combineLikeSources(sources);
    expect(combined).toHaveLength(2);
    // Sorted by best score: guide.pdf (0.9) first.
    expect(combined[0].filename).toBe("guide.pdf");
    expect(combined[0].references).toBe(2);
    expect(combined[0].bestSnippet).toBe("high");
    expect(combined[1].filename).toBe("notes.md");
    expect(combined[1].references).toBe(1);
  });
});

describe("MessageCitations", () => {
  it("renders nothing when there are no citations", () => {
    const { container } = render(<MessageCitations citations={[]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders a deduplicated source chip per document", () => {
    const sources: Citation[] = [
      { documentId: "d1", filename: "guide.pdf", snippet: "a", score: 0.9 },
      { documentId: "d1", filename: "guide.pdf", snippet: "b", score: 0.4 },
    ];
    render(<MessageCitations citations={sources} />);
    expect(screen.getByText("Sources")).toBeInTheDocument();
    expect(screen.getByText("guide.pdf")).toBeInTheDocument();
    // Two chunks from one document => an excerpt-count badge.
    expect(screen.getByText("2 excerpts")).toBeInTheDocument();
  });

  it("labels a fully injected document 'full document' instead of an excerpt count", () => {
    const sources: Citation[] = [
      {
        documentId: "d1",
        filename: "briefing.pdf",
        snippet: "whole deck",
        score: 1,
        full: true,
      },
    ];
    render(<MessageCitations citations={sources} />);
    expect(screen.getByText("briefing.pdf")).toBeInTheDocument();
    expect(screen.getByText("full document")).toBeInTheDocument();
    expect(screen.queryByText(/excerpt/)).not.toBeInTheDocument();
  });

  it("shows a Sources button that opens the sidebar with a card per web source", () => {
    const sources: Citation[] = [
      {
        documentId: "",
        filename: "Modal",
        snippet: "Modal runs Python.",
        score: 0,
        url: "https://modal.com",
        index: 2,
        title: "Modal docs",
      },
      {
        documentId: "",
        filename: "Truefoundry",
        snippet: "Deploy on k8s.",
        score: 0,
        url: "https://truefoundry.com",
        index: 1,
        title: "TrueFoundry",
      },
      // Duplicate URL collapses to one card.
      {
        documentId: "",
        filename: "Modal",
        snippet: "",
        score: 0,
        url: "https://modal.com",
        index: 3,
      },
    ];
    // Modal is [2] to the model but cited first, so it displays as 1.
    const display = new Map([
      [2, 1],
      [1, 2],
    ]);
    render(<MessageCitations citations={sources} display={display} />);
    // Closed: the sidebar dialog is not mounted; a Sources button is shown.
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /sources/i }));
    const dialog = screen.getByRole("dialog");
    expect(dialog).toBeInTheDocument();
    // One card per distinct source, in the order given. The caller supplies the
    // list already ordered by first citation (assignDisplayNumbers); re-sorting on
    // the persisted citation.index here would restore Tavily arrival order and
    // desync the cards from the [n] markers in the prose.
    const links = within(dialog).getAllByRole("link");
    expect(links).toHaveLength(2);
    expect(links[0]).toHaveTextContent("Modal docs");
    expect(links[0]).toHaveAttribute("href", "https://modal.com");
    expect(links[1]).toHaveTextContent("TrueFoundry");
    // The number comes from the display map, matching the inline marker.
    expect(links[0]).toHaveTextContent("1");
    expect(links[1]).toHaveTextContent("2");
    expect(within(dialog).getByText("Modal runs Python.")).toBeInTheDocument();
  });

  // Regression: messages persisted before documents were numbered carry no index at
  // all. Deriving the displayed list from the numbering dropped them entirely, so a
  // pure-RAG answer from before that change lost its whole Sources row.
  it("still shows documents from messages persisted before they were numbered", () => {
    const legacy: Citation[] = [
      { documentId: "d1", filename: "report.pdf", snippet: "a", score: 0.4 },
      { documentId: "d1", filename: "report.pdf", snippet: "b", score: 0.95 },
    ];

    render(<MessageCitations citations={legacy} display={new Map()} />);

    expect(screen.getByText("report.pdf")).toBeInTheDocument();
    expect(screen.getByText("2 excerpts")).toBeInTheDocument();
  });

  // combineLikeSources counts excerpts and picks the best-scoring snippet. Both are
  // unreachable if the list is collapsed to one entry per [n] before it runs, which
  // is why MessageCitations takes the full citation list rather than the numbering.
  it("counts every chunk of a cited document and shows its best snippet", () => {
    const chunks: Citation[] = [
      {
        documentId: "d1",
        filename: "report.pdf",
        snippet: "middling",
        score: 0.4,
        index: 1,
      },
      {
        documentId: "d1",
        filename: "report.pdf",
        snippet: "the best match",
        score: 0.95,
        index: 1,
      },
      {
        documentId: "d1",
        filename: "report.pdf",
        snippet: "weakest",
        score: 0.3,
        index: 1,
      },
    ];

    render(<MessageCitations citations={chunks} display={new Map([[1, 1]])} />);

    expect(screen.getByText("3 excerpts")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /report\.pdf/ }));
    expect(screen.getByText("the best match")).toBeInTheDocument();
  });

  // Documents are numbered in the same sequence as web sources, so the chip has to
  // show its marker for the reader to match it to the [n] in the prose.
  it("shows the citation number on a document chip", () => {
    const doc: Citation = {
      documentId: "d1",
      filename: "runbook.md",
      snippet: "Retention is 45 days.",
      score: 1,
      index: 1,
      full: true,
    };

    render(<MessageCitations citations={[doc]} display={new Map([[1, 1]])} />);

    const chip = screen.getByRole("button", { name: /runbook\.md/ });
    expect(chip).toHaveTextContent("1");
    expect(chip).toHaveTextContent("runbook.md");
  });

  it("reveals the matched snippet when a document chip is clicked", () => {
    const doc: Citation = {
      documentId: "d1",
      filename: "runbook.md",
      snippet: "Retention is 45 days.",
      score: 1,
      index: 1,
    };
    render(<MessageCitations citations={[doc]} display={new Map([[1, 1]])} />);

    expect(screen.queryByText("Retention is 45 days.")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /runbook\.md/ }));
    expect(screen.getByText("Retention is 45 days.")).toBeInTheDocument();

    // Clicking the same chip again collapses it.
    fireEvent.click(screen.getByRole("button", { name: /runbook\.md/ }));
    expect(screen.queryByText("Retention is 45 days.")).not.toBeInTheDocument();
  });

  it("omits the number on a document that was not cited", () => {
    const doc: Citation = {
      documentId: "d1",
      filename: "runbook.md",
      snippet: "x",
      score: 1,
      index: 4,
    };

    render(<MessageCitations citations={[doc]} display={new Map()} />);

    expect(
      screen.getByRole("button", { name: /runbook\.md/ }),
    ).not.toHaveTextContent("4");
  });

  // While streaming, the list also carries sources gathered but not yet cited.
  // Those have no marker in the prose, so numbering them by row position would
  // point the reader at a citation that does not exist.
  it("drops web sources the answer never cited", () => {
    const sources: Citation[] = [
      {
        documentId: "",
        filename: "Modal",
        snippet: "",
        score: 0,
        url: "https://modal.com",
        index: 2,
        title: "Modal docs",
      },
      {
        documentId: "",
        filename: "Truefoundry",
        snippet: "",
        score: 0,
        url: "https://truefoundry.com",
        index: 1,
        title: "TrueFoundry",
      },
    ];
    // Only Modal is cited. Truefoundry was gathered and passed over, so listing it
    // would imply an attribution the answer never makes.
    render(
      <MessageCitations citations={sources} display={new Map([[2, 1]])} />,
    );
    fireEvent.click(screen.getByRole("button", { name: /sources/i }));
    const links = within(screen.getByRole("dialog")).getAllByRole("link");

    expect(links).toHaveLength(1);
    expect(links[0]).toHaveTextContent("Modal docs");
    expect(links[0]).toHaveTextContent("1");
  });

  it("splits the sidebar with a More divider past the first four sources", () => {
    const sources: Citation[] = Array.from({ length: 6 }, (_, i) => ({
      documentId: "",
      filename: `Site${i + 1}`,
      snippet: "",
      score: 0,
      url: `https://site${i + 1}.com`,
      index: i + 1,
      title: `Title ${i + 1}`,
    }));
    render(<MessageCitations citations={sources} />);
    fireEvent.click(screen.getByRole("button", { name: /sources/i }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("More")).toBeInTheDocument();
    expect(within(dialog).getAllByRole("link")).toHaveLength(6);
  });

  it("renders document chips alongside the web Sources button", () => {
    const sources: Citation[] = [
      { documentId: "d1", filename: "guide.pdf", snippet: "a", score: 0.9 },
      {
        documentId: "",
        filename: "Modal",
        snippet: "",
        score: 0,
        url: "https://modal.com",
        index: 1,
      },
    ];
    render(<MessageCitations citations={sources} />);
    expect(screen.getByText("guide.pdf")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /sources/i }),
    ).toBeInTheDocument();
  });
});

describe("SourcesSidebar selection", () => {
  const sources: Citation[] = [
    { ...webSource("https://modal.com", 2, "Modal"), title: "Modal docs" },
    {
      ...webSource("https://truefoundry.com", 1, "Truefoundry"),
      title: "TrueFoundry",
    },
  ];
  // Modal is cited first, so it shows 1; TrueFoundry shows 2.
  const display = new Map([
    [2, 1],
    [1, 2],
  ]);

  function openDrawer(props: Partial<ComponentProps<typeof MessageCitations>>) {
    return render(
      <MessageCitations
        citations={sources}
        display={display}
        sourcesOpen
        {...props}
      />,
    );
  }

  it("marks only the pinned source's card", () => {
    openDrawer({ selected: new Set([1]) });

    const pinned = screen.getByRole("link", { name: /Modal docs/ });
    const other = screen.getByRole("link", { name: /TrueFoundry/ });
    expect(pinned).toHaveClass("ui-source-card-selected");
    expect(pinned).toHaveAttribute("aria-current", "true");
    expect(other).not.toHaveClass("ui-source-card-selected");
    expect(other).not.toHaveAttribute("aria-current");
  });

  // A run of markers pins several sources at once — that is the only way more than
  // one card is ever marked.
  it("marks every source of a pinned run", () => {
    openDrawer({ selected: new Set([1, 2]) });

    for (const name of [/Modal docs/, /TrueFoundry/])
      expect(screen.getByRole("link", { name })).toHaveClass(
        "ui-source-card-selected",
      );
  });

  it("reports the hovered card by its display number, and clears on leave", () => {
    const onHoverSource = vi.fn();
    openDrawer({ onHoverSource });

    const card = screen.getByRole("link", { name: /TrueFoundry/ });
    fireEvent.mouseEnter(card);
    expect(onHoverSource).toHaveBeenCalledWith(2);
    fireEvent.mouseLeave(card);
    expect(onHoverSource).toHaveBeenLastCalledWith(undefined);
  });

  // Hover writes no state: it must not disturb what a marker click pinned, which is
  // what makes clearing-on-mouse-move unnecessary.
  it("keeps a pinned card marked while another is hovered", () => {
    openDrawer({ selected: new Set([1]) });

    fireEvent.mouseEnter(screen.getByRole("link", { name: /TrueFoundry/ }));
    expect(screen.getByRole("link", { name: /Modal docs/ })).toHaveClass(
      "ui-source-card-selected",
    );
  });
});
