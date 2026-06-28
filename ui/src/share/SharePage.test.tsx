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
    { id: "m1", role: "user", content: "Compare them", createdAt: "2026-06-28T00:00:00Z" },
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
    vi.spyOn(api, "getPublicShare").mockRejectedValue(new api.ShareNotFoundError());
    render(<SharePage shareId="gone" />);

    expect(await screen.findByText(/isn.t available/i)).toBeInTheDocument();
  });
});
