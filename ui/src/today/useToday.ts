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

// NARROW is below Tailwind's sm: there the weather always starts as the line.
export const NARROW = "(max-width: 639px)";

function isNarrow(): boolean {
  return (
    typeof window.matchMedia === "function" && window.matchMedia(NARROW).matches
  );
}

// localDate is the browser's date, "YYYY-MM-DD".
function localDate(now = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

// useToday loads the start page's "Today" and keeps it current: it reloads
// when news is read elsewhere (the bell, a thread) and when the tab comes
// back on a new day or after a while, and reads runs itself with the card
// gone before the server answers.
export function useToday() {
  const [state, setState] = useState<TodayState>({ status: "loading" });
  // Runs read here stay hidden even if a reload races the read.
  const [read, setRead] = useState<ReadonlySet<string>>(() => new Set());
  const alive = useRef(true);
  // When and on which local date the last load answered.
  const loaded = useRef<{ at: number; date: string } | null>(null);
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
      // A phone shows the line whatever the server picks: asking compact
      // keeps the server from spending the day's tiles on it.
      const fresh = await getToday({ compact: isNarrow() });
      if (!alive.current || mine < applied.current) return;
      applied.current = mine;
      loaded.current = { at: Date.now(), date: localDate() };
      // The first answer of a day picks tiles or the line; a reload the same
      // day (news read, a place saved) must not fold the tiles under the user.
      setState((prev) =>
        prev.status === "ready" && prev.today.date === fresh.date
          ? { status: "ready", today: { ...fresh, view: prev.today.view } }
          : { status: "ready", today: fresh },
      );
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
    // Deferred so StrictMode's mount-unmount-mount sends one GET, not two:
    // the server picks tiles for the first visit of the day only.
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
      if (last.date !== localDate() || Date.now() - last.at > STALE_MS)
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
