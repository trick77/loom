import "@testing-library/jest-dom/vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { Citation } from "../api";
import { SourceFavicon } from "./SourceFavicon";

function web(extra: Partial<Citation>): Citation {
  return { documentId: "", filename: "Modal", snippet: "", score: 0, url: "https://modal.com", ...extra };
}

describe("SourceFavicon", () => {
  it("prefers a tool-provided favicon, then Google, then a letter avatar — both proxied", () => {
    const { container } = render(<SourceFavicon citation={web({ favicon: "https://cdn.example/icon.png" })} />);
    const img = () => container.querySelector("img");
    // 1) tool-provided favicon, routed through the backend cache proxy
    expect(img()!.getAttribute("src")).toBe(
      `/api/favicon?u=${encodeURIComponent("https://cdn.example/icon.png")}`,
    );
    // 2) on error -> Google favicon service for the host, also proxied
    fireEvent.error(img()!);
    const src2 = img()!.getAttribute("src")!;
    expect(src2).toContain("/api/favicon?u=");
    expect(decodeURIComponent(src2)).toContain("google.com/s2/favicons?domain=modal.com");
    // 3) on error again -> letter avatar (no <img>)
    fireEvent.error(img()!);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("M")).toBeInTheDocument();
  });

  it("starts at the proxied Google service when no favicon is provided", () => {
    const { container } = render(<SourceFavicon citation={web({ favicon: undefined })} />);
    const src = container.querySelector("img")!.getAttribute("src")!;
    expect(src).toContain("/api/favicon?u=");
    expect(decodeURIComponent(src)).toContain("google.com/s2/favicons?domain=modal.com");
  });

  it("falls straight to a letter avatar when there is no url", () => {
    const { container } = render(<SourceFavicon citation={web({ url: "", filename: "Guardian" })} />);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("G")).toBeInTheDocument();
  });
});
