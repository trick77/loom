import { useEffect, useMemo, useState } from "react";

import { listThreads, searchThreadContent, type Thread, type ThreadContentHit } from "../api";

export type SearchResult = { thread: Thread; snippet?: string };

// The sidebar search modal shows at most this many rows. The Threads page passes
// a larger limit so "Select all" over an active search covers every match.
export const MAX_SEARCH_RESULTS = 20;

// Title search is near-instant (a LIKE on a small column), so it fires on a
// short debounce; the full-text content search hits the whole message corpus
// and lags, so it waits a bit longer to avoid a request per keystroke.
const TITLE_DEBOUNCE_MS = 80;
const CONTENT_DEBOUNCE_MS = 250;

// useThreadSearch powers the claude.ai-style two-tier search. With an empty
// query it returns the most recent threads. While typing it merges fast
// title matches (shown first, no snippet) with the slower full-text content
// matches (appended, carrying a snippet); a title match that also matches by
// content gains that snippet. Results are deduped per thread and capped at
// `limit` (default MAX_SEARCH_RESULTS). Stale responses (the query moved on
// before they resolved) are discarded via the effect-cancellation guard.
//
// `reloadToken` lets a caller force a refetch after a mutation it made
// elsewhere (e.g. the Threads page deleting selected rows): bumping it re-runs
// the search so stale rows drop out.
export function useThreadSearch(
  query: string,
  options: { limit?: number; reloadToken?: number } = {},
): {
  results: SearchResult[];
  titleLoading: boolean;
  contentLoading: boolean;
} {
  const { limit = MAX_SEARCH_RESULTS, reloadToken = 0 } = options;
  const trimmed = query.trim();
  const [titleResults, setTitleResults] = useState<Thread[]>([]);
  const [contentResults, setContentResults] = useState<ThreadContentHit[]>([]);
  const [titleLoading, setTitleLoading] = useState(true);
  const [contentLoading, setContentLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setTitleLoading(true);

    const titleHandle = window.setTimeout(() => {
      const request =
        trimmed === ""
          ? listThreads({ limit })
          : listThreads({ search: trimmed, limit });
      request
        .then((page) => {
          if (cancelled) return;
          setTitleResults(page.items);
          setTitleLoading(false);
        })
        .catch(() => {
          if (cancelled) return;
          setTitleResults([]);
          setTitleLoading(false);
        });
    }, TITLE_DEBOUNCE_MS);

    let contentHandle: number | undefined;
    if (trimmed === "") {
      setContentResults([]);
      setContentLoading(false);
    } else {
      setContentLoading(true);
      contentHandle = window.setTimeout(() => {
        searchThreadContent({ query: trimmed, limit })
          .then((hits) => {
            if (cancelled) return;
            setContentResults(hits);
            setContentLoading(false);
          })
          .catch(() => {
            if (cancelled) return;
            setContentResults([]);
            setContentLoading(false);
          });
      }, CONTENT_DEBOUNCE_MS);
    }

    return () => {
      cancelled = true;
      window.clearTimeout(titleHandle);
      if (contentHandle !== undefined) window.clearTimeout(contentHandle);
    };
  }, [trimmed, limit, reloadToken]);

  const results = useMemo<SearchResult[]>(() => {
    const byId = new Map<string, SearchResult>();
    const order: string[] = [];
    for (const thread of titleResults) {
      if (byId.has(thread.id)) continue;
      byId.set(thread.id, { thread });
      order.push(thread.id);
    }
    for (const hit of contentResults) {
      const existing = byId.get(hit.thread.id);
      if (existing) {
        if (existing.snippet === undefined) existing.snippet = hit.snippet;
      } else {
        byId.set(hit.thread.id, { thread: hit.thread, snippet: hit.snippet });
        order.push(hit.thread.id);
      }
    }
    return order.slice(0, limit).map((id) => byId.get(id)!);
  }, [titleResults, contentResults, limit]);

  return { results, titleLoading, contentLoading };
}
