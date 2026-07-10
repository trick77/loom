import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Citation } from "../api";
import { SourceFavicon } from "./SourceFavicon";

function web(extra: Partial<Citation>): Citation {
  return { documentId: "", filename: "Modal", snippet: "", score: 0, url: "https://modal.com", ...extra };
}

describe("SourceFavicon", () => {
  it("requests the backend-resolved site icon for the source's page url", () => {
    const { container } = render(<SourceFavicon citation={web({})} />);
    const img = container.querySelector("img")!;
    expect(img.getAttribute("src")).toBe(`/api/favicon?u=${encodeURIComponent("https://modal.com")}`);
    // No lazy loading (tiny, in-view) and no dark placeholder background.
    expect(img.getAttribute("loading")).toBeNull();
    expect(img.className).not.toContain("bg-[#2a2a28]");
  });

  it("fades the icon in on load", () => {
    const { container } = render(<SourceFavicon citation={web({})} />);
    const img = () => container.querySelector("img")!;
    expect(img().className).toContain("opacity-0");
    fireEvent.load(img());
    expect(img().className).toContain("opacity-100");
  });

  it("falls back to a letter avatar when the icon fails to load", () => {
    const { container } = render(<SourceFavicon citation={web({})} />);
    fireEvent.error(container.querySelector("img")!);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("M")).toBeInTheDocument();
  });

  it("falls straight to a letter avatar when there is no url", () => {
    const { container } = render(<SourceFavicon citation={web({ url: "", filename: "Guardian" })} />);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("G")).toBeInTheDocument();
  });
});
