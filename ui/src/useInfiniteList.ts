import { useCallback, useEffect, useRef, useState } from "react";

import type { Page } from "./api";

// useInfiniteList drives cursor-based infinite scrolling: it loads the first
// page whenever resetKeys change (e.g. a new search/filter), appends further
// pages as a sentinel element scrolls into view, and surfaces load state.
//
// fetchPage receives the cursor for the page to load (null for the first page)
// and returns the standard {items, nextCursor} envelope. resetKeys behave like
// an effect dependency array: any change clears the list and reloads page one.
export function useInfiniteList<T>(
  fetchPage: (cursor: string | null) => Promise<Page<T>>,
  resetKeys: ReadonlyArray<unknown>,
) {
  const [items, setItems] = useState<T[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [cursor, setCursor] = useState<string | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const [error, setError] = useState<unknown>(null);

  // Hold the latest fetchPage without making it a reset dependency (it is a
  // fresh closure each render and would otherwise reload on every render).
  const fetchPageRef = useRef(fetchPage);
  fetchPageRef.current = fetchPage;

  // Monotonic request id: a reset bumps it so a slower in-flight request from a
  // previous filter cannot apply its results over the newer list.
  const requestSeq = useRef(0);

  useEffect(() => {
    const seq = ++requestSeq.current;
    setItems([]);
    setLoaded(false);
    setCursor(null);
    setHasMore(false);
    setError(null);
    setLoadingMore(true);
    fetchPageRef
      .current(null)
      .then((page) => {
        if (seq !== requestSeq.current) return;
        setItems(page.items);
        setCursor(page.nextCursor);
        setHasMore(page.nextCursor !== null);
        setLoaded(true);
      })
      .catch((err: unknown) => {
        if (seq !== requestSeq.current) return;
        setError(err);
      })
      .finally(() => {
        if (seq === requestSeq.current) setLoadingMore(false);
      });
    // resetKeys is the intended dependency list.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, resetKeys);

  const loadMore = useCallback(() => {
    if (loadingMore || !hasMore || cursor === null) return;
    const seq = requestSeq.current;
    setLoadingMore(true);
    fetchPageRef
      .current(cursor)
      .then((page) => {
        if (seq !== requestSeq.current) return;
        setItems((prev) => [...prev, ...page.items]);
        setCursor(page.nextCursor);
        setHasMore(page.nextCursor !== null);
      })
      .catch((err: unknown) => {
        if (seq !== requestSeq.current) return;
        setError(err);
      })
      .finally(() => {
        if (seq === requestSeq.current) setLoadingMore(false);
      });
  }, [cursor, hasMore, loadingMore]);

  // The observer is created once per sentinel node; route it through a ref so it
  // always invokes the latest loadMore (which closes over current cursor/state).
  const loadMoreRef = useRef(loadMore);
  loadMoreRef.current = loadMore;
  const observerRef = useRef<IntersectionObserver | null>(null);
  const nodeRef = useRef<HTMLElement | null>(null);

  const sentinelRef = useCallback((node: HTMLElement | null) => {
    observerRef.current?.disconnect();
    observerRef.current = null;
    nodeRef.current = node;
    if (node === null || typeof IntersectionObserver === "undefined") return;
    // rootMargin pre-loads slightly before the sentinel reaches the viewport.
    observerRef.current = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          loadMoreRef.current();
        }
      },
      { rootMargin: "300px" },
    );
    observerRef.current.observe(node);
  }, []);

  // After a page settles, re-observe the sentinel: IntersectionObserver only
  // fires on visibility *changes*, so a still-visible sentinel (e.g. a short
  // page that did not fill the viewport) would otherwise never load the next
  // page. Re-observing forces a fresh callback that keeps filling until the
  // viewport is covered or there is nothing more.
  useEffect(() => {
    if (loadingMore || !hasMore) return;
    const node = nodeRef.current;
    const observer = observerRef.current;
    if (node === null || observer === null) return;
    observer.unobserve(node);
    observer.observe(node);
  }, [loadingMore, hasMore, items.length]);

  useEffect(() => () => observerRef.current?.disconnect(), []);

  return { items, setItems, loaded, loadingMore, hasMore, error, sentinelRef, loadMore };
}
