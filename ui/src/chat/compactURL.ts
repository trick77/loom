// A URL shown as text never takes more than this many characters.
export const COMPACT_URL_MAX = 40;

// Scheme, credentials, "www.", host, path, query; the fragment is not kept. A
// regex, not new URL(): mid-stream the URL is cut off anywhere.
const PARTS =
  /^(?:[a-z][a-z0-9+.-]*:\/\/)?(?:[^/?#@]*@)?(?:www\.)?([^/?#]*)([^?#]*)(\?[^#]*)?/i;

// compactURL shortens a URL for display: a short one stays as written. A long
// one loses scheme, credentials, "www." and fragment, then — until it fits —
// all but the first query parameter, the query, and leading path segments
// ("github.com/…/blob/master/README.md"): the end of a path is what tells two
// pages of one site apart. Whatever still exceeds max is cut with "…".
export function compactURL(url: string, max = COMPACT_URL_MAX): string {
  if (length(url) <= max) return url;
  const match = PARTS.exec(url);
  const host = match?.[1] ?? "";
  if (host === "") return clip(url, max);
  const path = (match?.[2] ?? "").replace(/\/+$/, "");
  const query = match?.[3] ?? "";
  const firstParam = query.split("&")[0];
  const candidates = [host + path + query, host + path + firstParam, host + path];
  for (const candidate of candidates) {
    if (length(candidate) <= max) return candidate;
  }
  const segments = path.split("/").filter((segment) => segment !== "");
  if (segments.length < 2) return clip(host + path, max);
  const head = `${host}/…`;
  let tail = `/${segments[segments.length - 1]}`;
  for (let i = segments.length - 2; i >= 1; i--) {
    const longer = `/${segments[i]}${tail}`;
    if (length(head + longer) > max) break;
    tail = longer;
  }
  return clip(head + tail, max);
}

// length and clip count code points, so a cut never splits a surrogate pair.
function length(text: string): number {
  return Array.from(text).length;
}

function clip(text: string, max: number): string {
  const chars = Array.from(text);
  return chars.length <= max ? text : `${chars.slice(0, max - 1).join("")}…`;
}
