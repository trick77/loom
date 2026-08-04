import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../api";
import { SharePage } from "./SharePage";

const sample: api.PublicShare = {
  shareId: "tok123",
  title: "Comparing gateways",
  author: "Jan",
  sharedAt: "2026-06-28T00:00:00Z",
  messages: [
    {
      id: "m1",
      role: "user",
      content: "Compare them",
      createdAt: "2026-06-28T00:00:00Z",
    },
    {
      id: "m2",
      role: "assistant",
      content: "Here is the answer",
      contentBlocks: [{ type: "text", content: "Here is the answer" }],
      createdAt: "2026-06-28T00:00:01Z",
    },
  ],
};

describe("SharePage", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("renders the frozen transcript and the 'Shared by' attribution", async () => {
    vi.spyOn(api, "getPublicShare").mockResolvedValue(sample);
    render(<SharePage shareId="tok123" />);

    expect(await screen.findByText("Compare them")).toBeInTheDocument();
    expect(screen.getByText("Here is the answer")).toBeInTheDocument();
    expect(screen.getByText("Shared by Jan")).toBeInTheDocument();
    // Read-only: no composer/retry/copy affordances leak into the public view.
    expect(screen.queryByLabelText("Retry response")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Copy response")).not.toBeInTheDocument();
  });

  it("shows a not-found notice when the share is missing or disabled", async () => {
    vi.spyOn(api, "getPublicShare").mockRejectedValue(
      new api.ShareNotFoundError(),
    );
    render(<SharePage shareId="gone" />);

    expect(await screen.findByText(/isn.t available/i)).toBeInTheDocument();
  });
});

// The end of the chain the backend starts: web citations survive the share whitelist,
// so their markers have somewhere to go. A share has no drawer, so a marker links out
// to the page directly rather than opening a source list that is not there.
describe("SharePage citations", () => {
  beforeEach(() => vi.restoreAllMocks());

  const cited: api.PublicShare = {
    ...sample,
    messages: [
      {
        id: "m1",
        role: "assistant",
        content: "The page agrees [2].",
        contentBlocks: [{ type: "text", content: "The page agrees [2]." }],
        citations: [
          {
            documentId: "",
            filename: "Modal",
            snippet: "Modal runs Python.",
            score: 0,
            url: "https://modal.com/docs",
            index: 2,
            title: "Modal docs",
          },
        ],
        createdAt: "2026-06-28T00:00:01Z",
      },
    ],
  };

  it("renders a citation marker as an outbound link to its page", async () => {
    vi.spyOn(api, "getPublicShare").mockResolvedValue(cited);
    render(<SharePage shareId="tok123" />);

    const marker = await screen.findByRole("link", { name: "Modal" });
    expect(marker).toHaveClass("ui-source-pill");
    expect(marker).toHaveAttribute("href", "https://modal.com/docs");
    expect(marker).toHaveAttribute("target", "_blank");
    // The marker shows the reader-facing number, and the raw "[2]" is gone.
    expect(marker).toHaveTextContent("1");
    expect(screen.queryByText(/\[2\]/)).not.toBeInTheDocument();
  });

  // The drawer belongs to the authed transcript: it needs the full citation list,
  // which a share deliberately does not carry.
  it("does not render the sources row or drawer", async () => {
    vi.spyOn(api, "getPublicShare").mockResolvedValue(cited);
    render(<SharePage shareId="tok123" />);

    await screen.findByRole("link", { name: "Modal" });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.queryByText("Sources")).not.toBeInTheDocument();
  });

  // An older snapshot, frozen before citations were shared at all, must still render.
  it("renders a snapshot that carries no citations", async () => {
    vi.spyOn(api, "getPublicShare").mockResolvedValue(sample);
    render(<SharePage shareId="tok123" />);

    expect(await screen.findByText("Here is the answer")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
