import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Citation } from "../api";
import { SourceFavicon } from "./SourceFavicon";

function web(extra: Partial<Citation>): Citation {
  return { documentId: "", filename: "Modal", snippet: "", score: 0, url: "https://modal.com", ...extra };
}

describe("SourceFavicon", () => {
  it("prefers a tool-provided favicon, then Google, then a letter avatar", () => {
    const { container } = render(<SourceFavicon citation={web({ favicon: "https://cdn.example/icon.png" })} />);
    const img = () => container.querySelector("img");
    // 1) tool-provided favicon
    expect(img()).toHaveAttribute("src", "https://cdn.example/icon.png");
    // 2) on error -> Google favicon service for the host
    fireEvent.error(img()!);
    expect(img()!.getAttribute("src")).toContain("google.com/s2/favicons?domain=modal.com");
    // 3) on error again -> letter avatar (no <img>)
    fireEvent.error(img()!);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("M")).toBeInTheDocument();
  });

  it("starts at the Google service when no favicon is provided", () => {
    const { container } = render(<SourceFavicon citation={web({ favicon: undefined })} />);
    expect(container.querySelector("img")!.getAttribute("src")).toContain("google.com/s2/favicons?domain=modal.com");
  });

  it("falls straight to a letter avatar when there is no url", () => {
    const { container } = render(<SourceFavicon citation={web({ url: "", filename: "Guardian" })} />);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("G")).toBeInTheDocument();
  });
});
