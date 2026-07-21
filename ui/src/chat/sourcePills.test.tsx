import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Citation } from "../api";
import { ProseMarkdown } from "./messages";
import { webSourceMap } from "./sourcePills";

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
  const sources = webSourceMap([
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
  ]);

  it("replaces [n] markers with clickable source pills linking to the URL", () => {
    render(
      <ProseMarkdown sources={sources}>
        {"TrueFoundry deploys models [1]. Modal runs Python [2]."}
      </ProseMarkdown>,
    );
    const pill = screen.getByRole("link", { name: "Truefoundry" });
    expect(pill).toHaveAttribute("href", "https://truefoundry.com");
    expect(pill).toHaveAttribute("target", "_blank");
    expect(screen.getByRole("link", { name: "Modal" })).toHaveAttribute(
      "href",
      "https://modal.com",
    );
  });

  it("leaves unknown markers as literal text", () => {
    render(
      <ProseMarkdown sources={sources}>
        {"An out-of-range marker [7] stays as text."}
      </ProseMarkdown>,
    );
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText(/\[7\]/)).toBeInTheDocument();
  });

  it("does not create pills when no source map is provided", () => {
    render(<ProseMarkdown>{"Plain answer with a [1] marker."}</ProseMarkdown>);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText(/\[1\]/)).toBeInTheDocument();
  });

  it("collapses a repeated same-source cluster into one pill", () => {
    render(
      <ProseMarkdown sources={sources}>
        {"Repeated cite [1][1] here."}
      </ProseMarkdown>,
    );
    expect(screen.getAllByRole("link", { name: "Truefoundry" })).toHaveLength(
      1,
    );
  });

  it("caps inline pills at 3 distinct sources and drops the rest", () => {
    const many = webSourceMap([
      {
        documentId: "",
        filename: "One",
        snippet: "",
        score: 0,
        url: "https://one.com",
        index: 1,
      },
      {
        documentId: "",
        filename: "Two",
        snippet: "",
        score: 0,
        url: "https://two.com",
        index: 2,
      },
      {
        documentId: "",
        filename: "Three",
        snippet: "",
        score: 0,
        url: "https://three.com",
        index: 3,
      },
      {
        documentId: "",
        filename: "Four",
        snippet: "",
        score: 0,
        url: "https://four.com",
        index: 4,
      },
      {
        documentId: "",
        filename: "Five",
        snippet: "",
        score: 0,
        url: "https://five.com",
        index: 5,
      },
    ]);
    render(
      <ProseMarkdown sources={many}>
        {"A clustered answer [1][2][3][4][5]."}
      </ProseMarkdown>,
    );
    // Only the first three distinct sources render inline.
    expect(screen.getAllByRole("link")).toHaveLength(3);
    expect(screen.getByRole("link", { name: "Three" })).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "Four" }),
    ).not.toBeInTheDocument();
    // Overflow markers are removed from the prose, not left as literal text.
    expect(screen.queryByText(/\[4\]/)).not.toBeInTheDocument();
    expect(screen.queryByText(/\[5\]/)).not.toBeInTheDocument();
  });
});
