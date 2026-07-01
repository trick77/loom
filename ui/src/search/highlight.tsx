import { Fragment, type ReactNode } from "react";

// cleanResultText strips markdown formatting and emoji from rendered search
// results while protecting the most jarring technical content. Emphasis markers
// (`*`/`** `/`` ` ``) are removed when they touch text — covering both `**bold**`
// and the truncated openers FTS snippet windows leave behind — but a run
// isolated by spaces is treated as an operator and kept: `2 * 3`, `2 ** 3`,
// `~30h`. Block markers (`#`, `>`, `-`/`+`/`* `) are stripped only at a line
// start, so inline `a > b`, `1 + 2`, `rust - lang` keep their operators. Snake
// _case and __dunder__ are untouched (`_` is never stripped). Emoji are removed
// but text symbols (™ © ® ✔ →) stay. Known tradeoff: a leading-star token like
// `*.tsx`, `char *p`, or `**kwargs` loses its star in the preview. Applied only
// when rendering search titles/snippets — stored titles and the normal
// (non-search) thread list keep their original text. The « » FTS match markers
// always survive cleaning, so renderSnippet can still split on them.
//
// Memoized: a search list re-renders every row on each keystroke and each
// arrow-key selection change, so each distinct string is cleaned once and the
// result reused. The cache is bounded to keep a long session from growing it.
const cleanCache = new Map<string, string>();

export function cleanResultText(text: string): string {
  const cached = cleanCache.get(text);
  if (cached !== undefined) return cached;
  const cleaned = text
    // [label](url) / ![alt](url) → keep the label; « » excluded so a match
    // marker sitting inside a URL is never swallowed along with the link.
    .replace(/!?\[([^\]«»]*)\]\([^)«»]*\)/g, "$1")
    // strikethrough (paired) — leftover single `~` is kept (e.g. "~30h" approx)
    .replace(/~~([^~]+?)~~/g, "$1")
    // bold / italic / inline-code markers that TOUCH text. FTS snippets are
    // truncated windows, so an opening `**`/`` ` `` often has no closing pair in
    // view — pairing alone would leave it visible. A `*`/`` ` `` run is treated as
    // markdown when a non-space sits on either side; a run isolated by spaces is
    // an operator (`2 * 3`, `2 ** 3`) and is kept. (Tradeoff: a leading-star token
    // like `*.tsx`, `char *p`, or `**kwargs` loses its star in the preview.)
    .replace(/[*`]+/g, (run: string, offset: number, src: string) => {
      const before = src[offset - 1];
      const after = src[offset + run.length];
      const touchesText =
        (before !== undefined && !/\s/.test(before)) ||
        (after !== undefined && !/\s/.test(after));
      return touchesText ? "" : run;
    })
    // heading / blockquote / unordered-list markers, only at a line start so
    // inline `a > b`, `1 + 2`, `rust - lang` keep their operators
    .replace(/(^|\n)[ \t]{0,3}#{1,6}[ \t]+/g, "$1")
    .replace(/(^|\n)[ \t]{0,3}>+[ \t]?/g, "$1")
    .replace(/(^|\n)[ \t]{0,3}[-+*][ \t]+/g, "$1")
    // emoji: default-presentation pictographs, text-default ones forced to emoji
    // with VS16 (U+FE0F), and leftover skin-tone modifiers / ZWJ / VS16. Text-
    // presentation symbols (™ © ® ✔ ‼ →) are not pictographic emoji and stay.
    .replace(/\p{Extended_Pictographic}️|\p{Emoji_Presentation}|[\p{Emoji_Modifier}‍️]/gu, "")
    // collapse the whitespace the removals leave behind (incl. folded newlines)
    .replace(/\s+/g, " ")
    .trim();
  if (cleanCache.size >= 1000) cleanCache.clear();
  cleanCache.set(text, cleaned);
  return cleaned;
}

// highlightTerms bolds every case-insensitive occurrence of the query's terms
// inside `text` (used for thread titles, where the backend has no snippet to
// mark up). Each whitespace-separated term is matched independently as a
// substring, mirroring the title LIKE search. Callers only invoke this for an
// active search; with an empty query it returns the cleaned text unhighlighted.
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
  // the split below still finds them.
  let text = cleanResultText(snippet);
  // The backend FTS snippet() centres the match (long leading context, then the
  // «match»), but this subline is a single-line truncated span — too much lead
  // pushes the «match» off the right edge (invisible highlight), while zero lead
  // loses the context that makes a result readable. Keep a bounded amount of
  // leading context so the match reads mid-sentence (claude.ai does the same)
  // yet stays visible. Cut strictly before the first «, snapped to a word
  // boundary so we never slice mid-word or past the marker.
  const firstMark = text.indexOf("«");
  const leadBudget = 32; // chars of leading context to keep before the match
  if (firstMark > leadBudget) {
    let cut = firstMark - leadBudget;
    const space = text.indexOf(" ", cut);
    if (space !== -1 && space < firstMark) cut = space + 1;
    // A no-space lead leaves `cut` on a raw code-unit offset; nudge off an
    // orphaned low surrogate so we never slice an astral char into a U+FFFD.
    if ((text.charCodeAt(cut) & 0xfc00) === 0xdc00) cut++;
    text = "…" + text.slice(cut);
  }
  // Split on the « match » delimiters, keeping the captured match text.
  const parts = text.split(/«(.*?)»/g);
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

export function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
