import { Fragment, type ReactNode } from "react";

// highlightTerms bolds every case-insensitive occurrence of the query's terms
// inside `text` (used for thread titles, where the backend has no snippet to
// mark up). Each whitespace-separated term is matched independently as a
// substring, mirroring the title LIKE search. Returns the text unchanged when
// there is nothing to highlight.
export function highlightTerms(text: string, query: string): ReactNode {
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
  // Split on the « match » delimiters, keeping the captured match text.
  const parts = snippet.split(/«(.*?)»/g);
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
