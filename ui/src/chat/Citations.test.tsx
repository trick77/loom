import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

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

  it("renders web sources as link chips ordered by citation index, deduped by url", () => {
    const sources: Citation[] = [
      { documentId: "", filename: "Modal", snippet: "", score: 0, url: "https://modal.com", index: 2 },
      { documentId: "", filename: "Truefoundry", snippet: "", score: 0, url: "https://truefoundry.com", index: 1 },
      // Duplicate URL (same page cited twice) collapses to one chip.
      { documentId: "", filename: "Modal", snippet: "", score: 0, url: "https://modal.com", index: 3 },
    ];
    render(<MessageCitations citations={sources} />);
    expect(screen.getByText("Sources")).toBeInTheDocument();
    const links = screen.getAllByRole("link");
    expect(links).toHaveLength(2);
    // Ordered by index: Truefoundry (1) before Modal (2).
    expect(links[0]).toHaveTextContent("Truefoundry");
    expect(links[0]).toHaveAttribute("href", "https://truefoundry.com");
    expect(links[1]).toHaveTextContent("Modal");
  });

  it("renders both document and web sources together", () => {
    const sources: Citation[] = [
      { documentId: "d1", filename: "guide.pdf", snippet: "a", score: 0.9 },
      { documentId: "", filename: "Modal", snippet: "", score: 0, url: "https://modal.com", index: 1 },
    ];
    render(<MessageCitations citations={sources} />);
    expect(screen.getByText("guide.pdf")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Modal" })).toHaveAttribute("href", "https://modal.com");
  });
});
