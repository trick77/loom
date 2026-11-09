import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  announce,
  getToday,
  markAllNotificationsRead,
  markNotificationRead,
  NOTIFICATIONS_CHANGED,
  THREAD_NEWS_CHANGED,
  type Today,
} from "../api";

export type TodayState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "ready"; today: Today };

// useToday loads the start page's "Today" and keeps its unread runs current:
// it reloads when news is read elsewhere (the bell, a thread), and reads runs
// itself with the card gone before the server answers.
export function useToday() {
  const [state, setState] = useState<TodayState>({ status: "loading" });
  // Runs read here stay hidden even if a reload races the read.
  const [read, setRead] = useState<ReadonlySet<string>>(() => new Set());
  const alive = useRef(true);

  const load = useCallback(async () => {
    try {
      const fresh = await getToday();
      if (!alive.current) return;
      // The first load picks tiles or the line for this visit; a reload
      // (news read, a place saved) must not fold the tiles under the user.
      setState((prev) =>
        prev.status === "ready"
          ? { status: "ready", today: { ...fresh, view: prev.today.view } }
          : { status: "ready", today: fresh },
      );
    } catch {
      if (!alive.current) return;
      // A failed reload keeps what is showing.
      setState((prev) =>
        prev.status === "ready" ? prev : { status: "error" },
      );
    }
  }, []);

  useEffect(() => {
    alive.current = true;
    // Deferred so StrictMode's mount-unmount-mount sends one GET, not two:
    // the server picks tiles for the first visit of the day only.
    const first = window.setTimeout(() => void load(), 0);
    const onChanged = () => void load();
    window.addEventListener(NOTIFICATIONS_CHANGED, onChanged);
    return () => {
      alive.current = false;
      window.clearTimeout(first);
      window.removeEventListener(NOTIFICATIONS_CHANGED, onChanged);
    };
  }, [load]);

  const markRead = useCallback((id: string) => {
    setRead((prev) => new Set(prev).add(id));
    void markNotificationRead(id)
      .then(() => {
        announce(NOTIFICATIONS_CHANGED);
        announce(THREAD_NEWS_CHANGED);
      })
      .catch(() =>
        setRead((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        }),
      );
  }, []);

  const markAllRead = useCallback((ids: string[]) => {
    setRead((prev) => new Set([...prev, ...ids]));
    void markAllNotificationsRead()
      .then(() => {
        announce(NOTIFICATIONS_CHANGED);
        announce(THREAD_NEWS_CHANGED);
      })
      .catch(() =>
        setRead((prev) => {
          const next = new Set(prev);
          for (const id of ids) next.delete(id);
          return next;
        }),
      );
  }, []);

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

  return { state: visible, reload: load, markRead, markAllRead };
}
