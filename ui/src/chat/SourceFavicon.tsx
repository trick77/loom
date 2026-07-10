import { useState, type CSSProperties } from "react";

import type { Citation } from "../api";

// hostOf returns the lowercased hostname of a url, or "" if unparseable.
export function hostOf(url?: string): string {
  if (url === undefined || url === "") return "";
  try {
    return new URL(url).hostname.toLowerCase();
  } catch {
    return "";
  }
}

// siteIconURL routes a source's page url through the backend, which resolves the
// best icon for the site (apple-touch-icon / largest raster / favicon.ico / a
// rendered service icon), caches it, and serves it same-origin with a long
// Cache-Control. One request, one good icon — no client-side fallback chain that
// would flash a placeholder and then swap. The backend deliberately prefers opaque,
// high-res icons so they stay visible on the dark-only UI (a site's default favicon
// is often a dark, light-mode-only glyph).
function siteIconURL(pageURL: string): string {
  return `/api/favicon?u=${encodeURIComponent(pageURL)}`;
}

// Deterministic muted colours for the letter-avatar fallback.
const AVATAR_COLORS = ["#8a6d3b", "#4a7a8c", "#7a5a8c", "#5a8c6d", "#8c5a5a", "#5a6d8c", "#8c7a4a"];
function colorFor(seed: string): string {
  let h = 0;
  for (let i = 0; i < seed.length; i += 1) h = (h * 31 + seed.charCodeAt(i)) >>> 0;
  return AVATAR_COLORS[h % AVATAR_COLORS.length];
}

// SourceFavicon shows a web source's icon, resolved and cached by the backend, and
// falls back to a coloured letter avatar when the source has no url or no icon could
// be found. The image fades in on load over a transparent background, so there is no
// dark placeholder disc flashing before the icon paints.
export function SourceFavicon({
  citation,
  size = 16,
  className = "",
  style,
}: {
  citation: Citation;
  size?: number;
  className?: string;
  style?: CSSProperties;
}) {
  const host = hostOf(citation.url);
  const label = (citation.filename ?? host).trim();
  const src = citation.url !== undefined && host !== "" ? siteIconURL(citation.url) : "";
  const [failed, setFailed] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const dim = { width: `${size}px`, height: `${size}px`, flex: "0 0 auto" };

  if (src === "" || failed) {
    const letter = (label[0] ?? "?").toUpperCase();
    return (
      <span
        aria-hidden="true"
        className={`inline-grid place-items-center rounded-full font-sans font-semibold text-white ${className}`}
        style={{ ...dim, background: colorFor(label || "?"), fontSize: `${Math.round(size * 0.55)}px`, ...style }}
      >
        {letter}
      </span>
    );
  }
  return (
    <img
      src={src}
      alt=""
      decoding="async"
      className={`rounded-full object-cover transition-opacity duration-200 ${loaded ? "opacity-100" : "opacity-0"} ${className}`}
      style={{ ...dim, ...style }}
      onLoad={() => setLoaded(true)}
      onError={() => setFailed(true)}
    />
  );
}
