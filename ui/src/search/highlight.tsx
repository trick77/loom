import { Fragment, type ReactNode } from "react";

// cleanResultText strips the visual noise raw chat content drags into search
// results: markdown emphasis/code/heading/blockquote/list/link syntax and
// emoji. It is applied only when rendering search titles and snippets, so the
// stored titles and the normal (non-search) thread list keep their original
// text untouched. Deliberately conservative — it leaves `_` (snake_case),
// digits, and inline hyphens (well-known) alone to avoid mangling the technical
// content that dominates these conversations.
export function cleanResultText(text: string): string {
  return text
    // [label](url) and ![alt](url) → keep just the visible label
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    // bold / italic / inline-code / strikethrough markers
    .replace(/\*+/g, "")
    .replace(/`+/g, "")
    .replace(/~~/g, "")
    // heading hashes and blockquote markers at a line/segment start
    .replace(/(^|\s)#{1,6}\s+/g, "$1")
    .replace(/(^|\s)>+\s?/g, "$1")
    // unordered-list bullets ("- " / "+ ") at a line/segment start; the trailing
    // space requirement leaves hyphenated words like "well-known" intact
    .replace(/(^|\s)[-+]\s+/g, "$1")
    // emoji, skin-tone modifiers, variation selectors and ZWJ joiners
    .replace(/[\p{Extended_Pictographic}\p{Emoji_Modifier}\uFE0F\u200D]/gu, "")
    // collapse the whitespace the removals leave behind
    .replace(/\s{2,}/g, " ")
    .trim();
}

// highlightTerms bolds every case-insensitive occurrence of the query's terms
// inside `text` (used for thread titles, where the backend has no snippet to
// mark up). Each whitespace-separated term is matched independently as a
// substring, mirroring the title LIKE search. Returns the text unchanged when
// there is nothing to highlight.
export function highlightTerms(rawText: string, query: string): ReactNode {
  // Fall back to the raw text if cleaning empties it (e.g. an all-emoji title),
  // so a row never renders a blank title.
  const text = cleanResultText(rawText) || rawText;
  const terms = query
    .trim()
    .split(/\s+/)
    .filter((t) => t.length > 0)
    .map(escapeRegExp);
  if (terms.length === 0) return text;

  // Single alternation pass so overlapping/adjacent matches stay in order.
  const pattern = new RegExp(`(${terms.join("|")})`, "ig");
  const parts = text.split(pattern);
  return parts.map((part, i) =>
    // split() with a capture group yields the matched separators at odd indices.
    i % 2 === 1 ? (
      <strong key={i} className="font-semibold text-ink">
        {part}
      </strong>
    ) : (
      <Fragment key={i}>{part}</Fragment>
    ),
  );
}

// renderSnippet turns a backend FTS snippet — which wraps matched terms in
// « » — into React nodes with the matches bolded. Never uses
// dangerouslySetInnerHTML: the snippet is raw user/assistant content.
export function renderSnippet(snippet: string): ReactNode {
  // Strip markdown/emoji noise first; the « » match markers survive cleaning, so
  // the split below still finds them. Then split on the « match » delimiters,
  // keeping the captured match text.
  const parts = cleanResultText(snippet).split(/«(.*?)»/g);
  return parts.map((part, i) =>
    i % 2 === 1 ? (
      <strong key={i} className="font-semibold text-ink">
        {part}
      </strong>
    ) : (
      <Fragment key={i}>{part}</Fragment>
    ),
  );
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
