import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "../api";
import { UsagePanel } from "./UsagePanel";

const sample: api.Usage = {
  promptTokens: 100,
  completionTokens: 50,
  cachedTokens: 10,
  reasoningTokens: 5,
  totalTokens: 150,
  embeddingTokens: 88,
  embeddingRequests: 6,
  webSearches: 4,
  webFetches: 7,
  obscuraFetches: 2,
  imageGens: 1,
  threadsCreated: 9,
  projectsCreated: 3,
  userMemoryLength: 1234,
  userMemoryMax: 2000,
};

describe("UsagePanel", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("renders counters and memory length from the API", async () => {
    vi.spyOn(api, "getUsage").mockResolvedValue(sample);
    render(<UsagePanel />);

    expect(await screen.findByText("150")).toBeInTheDocument(); // total tokens
    expect(screen.getByText("88")).toBeInTheDocument(); // embedding tokens
    expect(screen.getByText("6")).toBeInTheDocument(); // embedding requests
    expect(screen.getByText("4")).toBeInTheDocument(); // web searches
    // getByText normalizes the thin space (U+202F) to a regular space.
    expect(screen.getByText("1 234 / 2 000")).toBeInTheDocument(); // memory length (thin-space grouped)
  });

  it("shows an error message when loading fails", async () => {
    vi.spyOn(api, "getUsage").mockRejectedValue(new Error("boom"));
    render(<UsagePanel />);

    expect(await screen.findByText("Failed to load usage.")).toBeInTheDocument();
  });
});
