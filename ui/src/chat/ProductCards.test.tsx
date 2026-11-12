import "@testing-library/jest-dom/vitest";
import { describe, expect, it } from "vitest";
import { fireEvent, render, screen, within } from "@testing-library/react";

import { formatPrice, ProductCards } from "./ProductCards";
import { MessageCitations } from "./Citations";
import type { Citation } from "../api";

const source = (
  url: string,
  index: number,
  extra: Partial<Citation> = {},
): Citation => ({
  documentId: "",
  filename: "Galaxus",
  snippet: "",
  score: 0,
  url,
  index,
  ...extra,
});

const charger = source("https://shop.example/p/charger", 3, {
  title: "Ugreen Nexode (65 W, 3 Ports)",
  image: "https://shop.example/img/charger.jpg",
  product: { price: 29.9, currency: "CHF", rating: 4.7, ratingCount: 853 },
});
const cable = source("https://shop.example/p/cable", 4, {
  title: "Digitec Kabel",
  product: { rating: 4 },
});
const article = source("https://news.example/a", 5, { title: "A review" });

describe("ProductCards", () => {
  it("shows only product sources, in order, each linking to its page", () => {
    render(
      <ProductCards
        sources={[charger, article, cable]}
        display={new Map([[3, 1]])}
      />,
    );
    const list = screen.getByRole("list", { name: "Products" });
    const links = within(list).getAllByRole("link");
    expect(links.map((a) => a.getAttribute("href"))).toEqual([
      "https://shop.example/p/charger",
      "https://shop.example/p/cable",
    ]);
    expect(links[0]).toHaveAttribute("target", "_blank");
    expect(links[0]).toHaveAttribute("rel", "noopener noreferrer");
    // The picture goes through loom's proxy, never straight to the shop.
    const img = links[0].querySelector("img");
    expect(img?.getAttribute("src")).toBe(
      "/api/source-image?u=" +
        encodeURIComponent("https://shop.example/img/charger.jpg"),
    );
    expect(within(links[0]).getByText("CHF 29.90")).toBeInTheDocument();
    expect(
      within(links[0]).getByRole("img", { name: "4.7 out of 5, 853 ratings" }),
    ).toBeInTheDocument();
    // The [n] the prose cites it by.
    expect(within(links[0]).getByText("1")).toBeInTheDocument();
    // No price listed, no picture: the card says so and keeps its rating.
    expect(within(links[1]).getByText("no price")).toBeInTheDocument();
    expect(links[1].querySelector("img")).toBeNull();
    expect(
      // No count known: none is announced (never "0 ratings").
      within(links[1]).getByRole("img", { name: "4.0 out of 5" }),
    ).toBeInTheDocument();
  });

  it("renders nothing without a product", () => {
    const { container } = render(<ProductCards sources={[article]} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("leaves the picture out when it fails to load", () => {
    render(<ProductCards sources={[charger]} />);
    const img = screen.getByRole("link").querySelector("img");
    expect(img).not.toBeNull();
    fireEvent.error(img!);
    expect(screen.getByRole("link").querySelector("img")).toBeNull();
  });

  it("sits under the answer with the sources row", () => {
    render(<MessageCitations citations={[charger, article]} />);
    expect(screen.getByRole("list", { name: "Products" })).toBeInTheDocument();
    expect(screen.getByText("Sources")).toBeInTheDocument();
  });
});

describe("formatPrice", () => {
  it("formats in the reader's language", () => {
    // Intl puts a no-break space between code and amount.
    expect(formatPrice({ price: 33, currency: "CHF" }, "en")).toMatch(
      /^CHF\s33\.00$/,
    );
    expect(formatPrice({ price: 33, currency: "EUR" }, "de")).toMatch(
      /^33,00\s€$/,
    );
  });

  it("has no price without one or with an unknown currency", () => {
    expect(formatPrice({ rating: 4 }, "en")).toBeUndefined();
    expect(formatPrice({ price: 3, currency: "nope" }, "en")).toBeUndefined();
  });
});
