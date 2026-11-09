import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  announce,
  getToday,
  markNotificationRead,
  NOTIFICATIONS_CHANGED,
  THREAD_NEWS_CHANGED,
  type Today,
} from "../api";

export type TodayState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "ready"; today: Today };

// STALE_MS is how old a load may get before a return to the tab reloads it.
export const STALE_MS = 30 * 60_000;

// MAX_REFRESH_MS caps the wait for refreshAt: a far or odd time from the
// server still gets a reload, which then schedules the next one.
export const MAX_REFRESH_MS = 6 * 60 * 60_000;

// refreshTime reads refreshAt as epoch milliseconds; null when absent or not
// a time.
function refreshTime(refreshAt: string | null): number | null {
  if (refreshAt === null) return null;
  const at = Date.parse(refreshAt);
  return Number.isNaN(at) ? null : at;
}

// useToday loads the start page's "Today" and keeps it current: it reloads
// when the shown day changes (the server's refreshAt: 20:00, midnight), when
// news is read elsewhere (the bell, a thread), and when the tab comes back
// after that moment or after a while; it reads runs itself with the card
// gone before the server answers.
export function useToday() {
  const [state, setState] = useState<TodayState>({ status: "loading" });
  // Runs read here stay hidden even if a reload races the read.
  const [read, setRead] = useState<ReadonlySet<string>>(() => new Set());
  const alive = useRef(true);
  // When the last load answered, and when its shown day ends.
  const loaded = useRef<{ at: number; refreshAt: number | null } | null>(null);
  // True while this hook announces: the bell and sidebar need those events,
  // a reload here does not (the cards are already gone). dispatchEvent runs
  // the listeners synchronously, so the flag covers exactly those events.
  const announcing = useRef(false);

  // Loads are numbered; an answer older than one already shown is dropped.
  const sent = useRef(0);
  const applied = useRef(0);
  const pending = useRef(0);

  const load = useCallback(async () => {
    const mine = ++sent.current;
    pending.current++;
    try {
      const fresh = await getToday();
      if (!alive.current || mine < applied.current) return;
      applied.current = mine;
      const refreshAt = refreshTime(fresh.refreshAt);
      loaded.current = { at: Date.now(), refreshAt };
      setState({ status: "ready", today: fresh });
    } catch {
      if (!alive.current || mine < applied.current) return;
      // A failed reload keeps what is showing.
      setState((prev) =>
        prev.status === "ready" ? prev : { status: "error" },
      );
    } finally {
      pending.current--;
    }
  }, []);

  useEffect(() => {
    alive.current = true;
    // Deferred so StrictMode's mount-unmount-mount sends one GET, not two.
    const first = window.setTimeout(() => void load(), 0);
    // The bell sends NOTIFICATIONS_CHANGED or THREAD_NEWS_CHANGED when news
    // was read or found there; this hook's own announcements are skipped.
    const onChanged = () => {
      if (!announcing.current) void load();
    };
    const onVisible = () => {
      if (document.visibilityState !== "visible") return;
      const last = loaded.current;
      // Nothing loaded yet: retry, unless the first load is still on its way.
      if (last === null) {
        if (sent.current > 0 && pending.current === 0) void load();
        return;
      }
      // A sleeping tab may have missed the refreshAt timer.
      const now = Date.now();
      if (
        now - last.at > STALE_MS ||
        (last.refreshAt !== null && now >= last.refreshAt)
      )
        void load();
    };
    window.addEventListener(NOTIFICATIONS_CHANGED, onChanged);
    window.addEventListener(THREAD_NEWS_CHANGED, onChanged);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      alive.current = false;
      window.clearTimeout(first);
      window.removeEventListener(NOTIFICATIONS_CHANGED, onChanged);
      window.removeEventListener(THREAD_NEWS_CHANGED, onChanged);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [load]);

  // Reload when the shown day changes (refreshAt). Every load sets a new
  // state, so the timer is reset per load; a past or missing time schedules
  // nothing.
  useEffect(() => {
    if (state.status !== "ready") return;
    const at = refreshTime(state.today.refreshAt);
    if (at === null) return;
    const wait = at - Date.now();
    if (wait <= 0) return;
    const timer = window.setTimeout(
      () => void load(),
      Math.min(wait, MAX_REFRESH_MS),
    );
    return () => window.clearTimeout(timer);
  }, [state, load]);

  // markRead reads the given runs, and only those: "mark all" here means
  // the cards shown, not news the user never saw. A card whose read failed
  // comes back.
  const markRead = useCallback((ids: string[]) => {
    setRead((prev) => new Set([...prev, ...ids]));
    void Promise.allSettled(ids.map((id) => markNotificationRead(id))).then(
      (results) => {
        const failed = ids.filter((_, i) => results[i].status === "rejected");
        if (failed.length > 0) {
          setRead((prev) => {
            const next = new Set(prev);
            for (const id of failed) next.delete(id);
            return next;
          });
        }
        if (failed.length === ids.length) return;
        announcing.current = true;
        try {
          announce(NOTIFICATIONS_CHANGED);
          announce(THREAD_NEWS_CHANGED);
        } finally {
          announcing.current = false;
        }
      },
    );
  }, []);

  const markOneRead = useCallback((id: string) => markRead([id]), [markRead]);

  const visible = useMemo<TodayState>(
    () =>
      state.status === "ready" && read.size > 0
        ? {
            status: "ready",
            today: {
              ...state.today,
              runs: state.today.runs.filter((run) => !read.has(run.id)),
            },
          }
        : state,
    [state, read],
  );

  return {
    state: visible,
    reload: load,
    markRead: markOneRead,
    markAllRead: markRead,
  };
}
