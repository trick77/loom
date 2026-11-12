import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { Citation, CitationProduct } from "../api";
import type { DisplayMap } from "./sourcePills";
import { sourceImageURL } from "./SourcesSidebar";

// ProductCards shows the product pages behind an answer as a row of cards:
// picture, name, price and rating, each linking to its page. It takes the web
// sources in the order the sources row shows them (cited first, or all when
// the answer cites none) and keeps the ones a shop marked as products.
export function ProductCards({
  sources,
  display,
}: {
  sources: Citation[];
  display?: DisplayMap;
}) {
  const { t } = useTranslation();
  const products = sources.filter(
    (s): s is Citation & { url: string; product: CitationProduct } =>
      s.product !== undefined && s.url !== undefined && s.url !== "",
  );
  if (products.length === 0) return null;
  return (
    <ul
      aria-label={t("citations.products")}
      className="-mx-1 flex snap-x gap-2 overflow-x-auto px-1 pb-1"
    >
      {products.map((source) => (
        <li key={source.url} className="w-[140px] shrink-0 snap-start">
          <ProductCard
            source={source}
            number={display?.get(source.index ?? 0)}
          />
        </li>
      ))}
    </ul>
  );
}

function ProductCard({
  source,
  number,
}: {
  source: Citation & { url: string; product: CitationProduct };
  number?: number;
}) {
  const { t, i18n } = useTranslation();
  const { product } = source;
  const title = source.title?.trim() || source.url;
  const price = formatPrice(product, i18n.language);
  const rated = product.rating !== undefined && product.rating > 0;
  return (
    <a
      href={source.url}
      target="_blank"
      rel="noopener noreferrer"
      title={title}
      className="flex h-full flex-col overflow-hidden rounded-ui border border-border bg-card transition-colors hover:border-border-strong hover:bg-raised"
    >
      <span className="relative flex h-24 items-center justify-center bg-ink p-1.5">
        {source.image !== undefined && source.image !== "" && (
          <ProductImage image={source.image} />
        )}
        {number !== undefined && (
          <span className="absolute left-1 top-1 rounded-full bg-raised px-1.5 text-[11px] leading-[18px] tabular-nums text-ink-dim">
            {number}
          </span>
        )}
      </span>
      <span className="flex flex-1 flex-col gap-1 px-2 py-1.5">
        <span className="line-clamp-2 text-[12px] leading-snug text-ink-soft">
          {title}
        </span>
        {/* Wraps rather than clips: a four-digit count beside a price is
            wider than the card. */}
        <span className="mt-auto flex flex-wrap items-baseline justify-between gap-x-1 text-[12px] tabular-nums">
          <span
            className={`whitespace-nowrap ${price ? "font-medium text-ink" : "text-muted"}`}
          >
            {price ?? t("citations.noPrice")}
          </span>
          {rated && (
            // role=img: a plain span may not carry a name, and a screen
            // reader would read "black star 4.7 1118" instead.
            <span
              role="img"
              className="whitespace-nowrap text-muted"
              aria-label={t("citations.rating", {
                rating: product.rating?.toFixed(1),
                count: product.ratingCount ?? 0,
              })}
            >
              <span className="text-sun">★</span> {product.rating?.toFixed(1)}
              {product.ratingCount !== undefined && product.ratingCount > 0 && (
                <span className="text-[11px]"> ({product.ratingCount})</span>
              )}
            </span>
          )}
        </span>
      </span>
    </a>
  );
}

// ProductImage is the product's picture through the backend's image proxy,
// fitted whole into the card (shop pictures come tall, wide and square). One
// that fails to load leaves the well empty.
function ProductImage({ image }: { image: string }) {
  const [failed, setFailed] = useState(false);
  if (failed) return null;
  return (
    <img
      src={sourceImageURL(image)}
      alt=""
      loading="lazy"
      onError={() => setFailed(true)}
      className="max-h-full max-w-full object-contain"
    />
  );
}

// formatPrice is the price in the reader's language, or undefined when the
// shop listed none (or its currency is no code Intl knows).
export function formatPrice(
  product: CitationProduct,
  language: string,
): string | undefined {
  if (product.price === undefined || product.currency === undefined) {
    return undefined;
  }
  try {
    return new Intl.NumberFormat(language, {
      style: "currency",
      currency: product.currency,
    }).format(product.price);
  } catch {
    return undefined;
  }
}
