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
    // Two chunks from one document => a ×2 reference badge.
    expect(screen.getByText("×2")).toBeInTheDocument();
  });
});
