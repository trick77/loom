import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

import { AttachmentPreview, isRevocablePreview } from "./AttachmentPreview";

describe("isRevocablePreview", () => {
  it("treats only blob: object URLs as revocable", () => {
    expect(isRevocablePreview("blob:http://localhost/abc")).toBe(true);
  });

  it("never revokes stable server download URLs", () => {
    expect(isRevocablePreview("/api/artifacts/a1/download")).toBe(false);
  });

  it("returns false for an absent preview URL", () => {
    expect(isRevocablePreview(undefined)).toBe(false);
  });
});

describe("AttachmentPreview", () => {
  it("renders a thumbnail for an image with a preview URL", () => {
    const { container } = render(
      <AttachmentPreview mimeType="image/png" filename="photo.png" previewUrl="blob:preview" alt="a photo" />,
    );
    const img = container.querySelector("img");
    expect(img).not.toBeNull();
    expect(img?.getAttribute("src")).toBe("blob:preview");
    expect(img?.getAttribute("alt")).toBe("a photo");
  });

  it("falls back to the typed icon when the image fails to load", () => {
    const { container } = render(
      <AttachmentPreview mimeType="image/svg+xml" filename="diagram.svg" previewUrl="blob:broken" />,
    );
    expect(container.querySelector("img")).not.toBeNull();
    fireEvent.error(container.querySelector("img")!);
    // svg is not an accepted extension, so the fallback is the generic file glyph.
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("svg")).not.toBeNull();
  });

  it("recovers from a broken image when handed a fresh preview URL", () => {
    // Regression: a once-failed image must clear its broken state when the same
    // instance receives a new URL (blob: swapped for the stable server URL).
    const { container, rerender } = render(
      <AttachmentPreview mimeType="image/png" filename="photo.png" previewUrl="blob:stale" />,
    );
    fireEvent.error(container.querySelector("img")!);
    expect(container.querySelector("img")).toBeNull();
    rerender(
      <AttachmentPreview mimeType="image/png" filename="photo.png" previewUrl="/api/artifacts/a1/download" />,
    );
    const img = container.querySelector("img");
    expect(img).not.toBeNull();
    expect(img?.getAttribute("src")).toBe("/api/artifacts/a1/download");
  });

  it("overlays the extension pill on an image only when overlayLabel is set", () => {
    const { container, rerender } = render(
      <AttachmentPreview mimeType="image/png" filename="photo.png" previewUrl="blob:preview" overlayLabel />,
    );
    expect(screen.getByText("PNG")).not.toBeNull();
    rerender(<AttachmentPreview mimeType="image/png" filename="photo.png" previewUrl="blob:preview" />);
    expect(container.querySelector("img")).not.toBeNull();
    expect(screen.queryByText("PNG")).toBeNull();
  });

  it("renders an extension pill for a recognised non-image attachment", () => {
    const { container } = render(<AttachmentPreview mimeType="application/pdf" filename="report.pdf" />);
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("PDF")).not.toBeNull();
  });

  it("renders the generic file glyph for an unrecognised extension", () => {
    const { container } = render(<AttachmentPreview mimeType="application/octet-stream" filename="data.bin" />);
    expect(container.querySelector("img")).toBeNull();
    expect(container.querySelector("svg")).not.toBeNull();
  });
});
