import { useMemo, useState, type CSSProperties } from "react";

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

function googleFavicon(host: string, sizePx: number): string {
  return `https://www.google.com/s2/favicons?domain=${encodeURIComponent(host)}&sz=${sizePx}`;
}

// faviconProxy routes a remote favicon url through the backend so the bytes are
// fetched once, cached on disk, and served with a long browser Cache-Control —
// avoiding repeated third-party fetches and load flicker on every re-render.
export function faviconProxy(u: string): string {
  return `/api/favicon?u=${encodeURIComponent(u)}`;
}

// Deterministic muted colours for the letter-avatar fallback.
const AVATAR_COLORS = ["#8a6d3b", "#4a7a8c", "#7a5a8c", "#5a8c6d", "#8c5a5a", "#5a6d8c", "#8c7a4a"];
function colorFor(seed: string): string {
  let h = 0;
  for (let i = 0; i < seed.length; i += 1) h = (h * 31 + seed.charCodeAt(i)) >>> 0;
  return AVATAR_COLORS[h % AVATAR_COLORS.length];
}

// SourceFavicon shows a web source's icon, trying (1) a tool-provided favicon url,
// then (2) Google's favicon service, and finally (3) a coloured letter avatar when
// both fail to load.
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
  const chain = useMemo(
    () =>
      [citation.favicon, host === "" ? "" : googleFavicon(host, Math.max(32, size * 2))]
        .filter((u): u is string => typeof u === "string" && u !== "")
        .map(faviconProxy),
    [citation.favicon, host, size],
  );
  const [step, setStep] = useState(0);
  const dim = { width: `${size}px`, height: `${size}px`, flex: "0 0 auto" };

  if (step >= chain.length) {
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
      src={chain[step]}
      alt=""
      loading="lazy"
      className={`rounded-full bg-[#2a2a28] object-cover ${className}`}
      style={{ ...dim, ...style }}
      onError={() => setStep((s) => s + 1)}
    />
  );
}
