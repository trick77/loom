import "@testing-library/jest-dom/vitest";
import { describe, expect, it } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";

import { combineLikeSources, MessageCitations } from "./Citations";
import type { Citation } from "../api";

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
      { documentId: "d1", filename: "briefing.pdf", snippet: "whole deck", score: 1, full: true },
    ];
    render(<MessageCitations citations={sources} />);
    expect(screen.getByText("briefing.pdf")).toBeInTheDocument();
    expect(screen.getByText("full document")).toBeInTheDocument();
    expect(screen.queryByText(/excerpt/)).not.toBeInTheDocument();
  });

  it("shows a Sources button that opens the sidebar with a card per web source", () => {
    const sources: Citation[] = [
      { documentId: "", filename: "Modal", snippet: "Modal runs Python.", score: 0, url: "https://modal.com", index: 2, title: "Modal docs" },
      { documentId: "", filename: "Truefoundry", snippet: "Deploy on k8s.", score: 0, url: "https://truefoundry.com", index: 1, title: "TrueFoundry" },
      // Duplicate URL collapses to one card.
      { documentId: "", filename: "Modal", snippet: "", score: 0, url: "https://modal.com", index: 3 },
    ];
    render(<MessageCitations citations={sources} />);
    // Closed: the sidebar dialog is not mounted; a Sources button is shown.
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /sources/i }));
    const dialog = screen.getByRole("dialog");
    expect(dialog).toBeInTheDocument();
    // One card per distinct source, ordered by index (Truefoundry #1 before Modal #2).
    const links = within(dialog).getAllByRole("link");
    expect(links).toHaveLength(2);
    expect(links[0]).toHaveTextContent("TrueFoundry");
    expect(links[0]).toHaveAttribute("href", "https://truefoundry.com");
    expect(within(dialog).getByText("Modal runs Python.")).toBeInTheDocument();
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
      { documentId: "", filename: "Modal", snippet: "", score: 0, url: "https://modal.com", index: 1 },
    ];
    render(<MessageCitations citations={sources} />);
    expect(screen.getByText("guide.pdf")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /sources/i })).toBeInTheDocument();
  });
});
