// A URL shown as text never takes more than this many characters.
export const COMPACT_URL_MAX = 40;

// Scheme, "www.", host, then the path; query and fragment are not kept. A regex,
// not new URL(): mid-stream the URL is cut off anywhere.
const PARTS = /^(?:[a-z][a-z0-9+.-]*:\/\/)?(?:www\.)?([^/?#]*)([^?#]*)/i;

// compactURL shortens a URL for display: a short one stays as written. A long
// one loses scheme, "www.", query and fragment; if that is still too long, the
// path collapses to its last segment ("docs.example.com/…/page"), and whatever
// still exceeds max is cut with "…".
export function compactURL(url: string, max = COMPACT_URL_MAX): string {
  if (url.length <= max) return url;
  const match = PARTS.exec(url);
  const host = match?.[1] ?? "";
  const path = (match?.[2] ?? "").replace(/\/+$/, "");
  if (host === "") return clip(url, max);
  const segments = path.split("/").filter((segment) => segment !== "");
  if (host.length + path.length <= max || segments.length < 2)
    return clip(host + path, max);
  return clip(`${host}/…/${segments[segments.length - 1]}`, max);
}

function clip(text: string, max: number): string {
  return text.length <= max ? text : `${text.slice(0, max - 1)}…`;
}
