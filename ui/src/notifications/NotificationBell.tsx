import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";

import {
  listNotifications,
  markAllNotificationsRead,
  announce,
  markNotificationRead,
  NOTIFICATIONS_CHANGED,
  THREAD_NEWS_CHANGED,
  unreadNotificationCount,
  type AppNotification,
} from "../api";
import { AuthExpiredError } from "../api/http";
import { Icon } from "../chat/Icon";
import { useEscapeKey } from "../chat/useEscapeKey";
import { formatTimeAgo } from "../timeago";

// POLL_MS is how often the unread count is read; it is read on focus too.
export const POLL_MS = 60_000;

type Toast =
  | { kind: "one"; notification: AppNotification }
  | { kind: "many"; count: number };

export type Placement = "header" | "rail";

// The bell and its popover are found by attribute: the state lives in a hook
// the sidebar calls once, while the bell itself moves between header and rail.
const BELL = "[data-notification-bell]";
const PANEL = "[data-notification-panel]";

export type Notifications = ReturnType<typeof useNotifications>;

// useNotifications holds the bell's state: the unread count, the inbox and the
// toast a poll raises when it finds something new while the app is open. The
// sidebar calls it once, so collapsing the rail, which swaps the header bell
// for the rail bell, keeps a toast up: it stays until the user clicks it away.
export function useNotifications({
  onOpen,
  placement,
}: {
  onOpen(notification: AppNotification): void;
  placement: Placement;
}) {
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<AppNotification[] | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [onlyUnread, setOnlyUnread] = useState(false);
  const [toast, setToast] = useState<Toast | null>(null);
  const [panelStyle, setPanelStyle] = useState({ top: 48, left: 16 });
  const known = useRef<number | null>(null);

  const load = useCallback(async () => {
    try {
      const list = await listNotifications();
      setItems(list);
      setLoadError(false);
      return list;
    } catch {
      setLoadError(true);
      return null;
    }
  }, []);

  const poll = useCallback(async () => {
    let count: number;
    try {
      count = await unreadNotificationCount();
    } catch (error) {
      if (error instanceof AuthExpiredError) return;
      return;
    }
    const before = known.current;
    known.current = count;
    setUnread(count);
    // News read elsewhere (its thread, another tab) takes the toast with it.
    if (before !== null && count < before) setToast(null);
    if (before !== null && count > before) {
      // The sidebar picks up the new run threads and their dots.
      announce(THREAD_NEWS_CHANGED);
      const fresh = count - before;
      const list = await load();
      const newest = list?.find((n) => n.readAt === null);
      // A toast still up counts the new news in rather than losing its own.
      setToast((current) =>
        current !== null
          ? {
              kind: "many",
              count: (current.kind === "one" ? 1 : current.count) + fresh,
            }
          : fresh > 1 || !newest
            ? { kind: "many", count: fresh }
            : { kind: "one", notification: newest },
      );
    }
  }, [load]);

  useEffect(() => {
    const first = window.setTimeout(() => void poll(), 0);
    const handle = window.setInterval(() => void poll(), POLL_MS);
    const onFocus = () => void poll();
    window.addEventListener("focus", onFocus);
    window.addEventListener(NOTIFICATIONS_CHANGED, onFocus);
    return () => {
      window.clearTimeout(first);
      window.clearInterval(handle);
      window.removeEventListener("focus", onFocus);
      window.removeEventListener(NOTIFICATIONS_CHANGED, onFocus);
    };
  }, [poll]);

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: PointerEvent) => {
      const target = event.target as Element;
      if (target.closest?.(`${BELL}, ${PANEL}`)) return;
      setOpen(false);
    };
    document.addEventListener("pointerdown", onPointer);
    return () => document.removeEventListener("pointerdown", onPointer);
  }, [open]);
  useEscapeKey(() => setOpen(false), { active: open });
  useEscapeKey(() => setToast(null), { active: toast !== null && !open });

  const toggle = () => {
    if (!open) {
      // The one bell mounted, header or rail; the toast opens the inbox too.
      const rect = document.querySelector(BELL)?.getBoundingClientRect();
      setPanelStyle(
        rect === undefined
          ? { top: 48, left: 16 }
          : placement === "header"
            ? { top: rect.bottom + 6, left: Math.max(8, rect.left - 12) }
            : { top: Math.max(8, rect.top - 8), left: rect.right + 10 },
      );
      void load();
    }
    setOpen(!open);
  };

  const openItem = (notification: AppNotification) => {
    setOpen(false);
    setToast(null);
    if (notification.readAt === null) {
      setItems(
        (current) =>
          current?.map((n) =>
            n.id === notification.id
              ? { ...n, readAt: new Date().toISOString() }
              : n,
          ) ?? null,
      );
      setUnread((n) => Math.max(0, n - 1));
      known.current = Math.max(0, (known.current ?? 1) - 1);
      void markNotificationRead(notification.id)
        .then(() => announce(THREAD_NEWS_CHANGED))
        .catch(() => undefined);
    }
    onOpen(notification);
  };

  const readAll = () => {
    setToast(null);
    setItems(
      (current) =>
        current?.map((n) => ({
          ...n,
          readAt: n.readAt ?? new Date().toISOString(),
        })) ?? null,
    );
    setUnread(0);
    known.current = 0;
    void markAllNotificationsRead()
      .then(() => announce(THREAD_NEWS_CHANGED))
      .catch(() => undefined);
  };

  return {
    unread,
    open,
    items,
    loadError,
    onlyUnread,
    setOnlyUnread,
    toast,
    dismissToast: () => setToast(null),
    panelStyle,
    toggle,
    close: () => setOpen(false),
    openItem,
    readAll,
  };
}

// NotificationBell is the sidebar's bell: a gold dot while anything is unread
// and the inbox in a popover.
export function NotificationBell({
  notifications: n,
  placement,
  onManageSchedules,
}: {
  notifications: Notifications;
  placement: Placement;
  onManageSchedules(): void;
}) {
  const { t } = useTranslation();
  const shown = (n.items ?? []).filter(
    (item) => !n.onlyUnread || item.readAt === null,
  );
  const unreadInList = (n.items ?? []).filter(
    (item) => item.readAt === null,
  ).length;

  return (
    <>
      <button
        data-notification-bell=""
        type="button"
        aria-label={t(
          n.unread > 0 ? "notifications.bellUnread" : "notifications.bell",
        )}
        aria-expanded={n.open}
        onClick={n.toggle}
        className={`relative grid place-items-center rounded transition-colors hover:text-white ${n.open ? "text-white" : ""}`}
      >
        <Icon name="bell" size={placement === "header" ? "18px" : "20px"} />
        {n.unread > 0 && (
          <span
            data-testid="notification-dot"
            className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full bg-[#c9a25f] ring-2 ring-[#1b1b1a]"
          />
        )}
      </button>
      {n.open &&
        createPortal(
          <div
            data-notification-panel=""
            role="dialog"
            aria-label={t("notifications.title")}
            style={n.panelStyle}
            className="fixed z-50 w-[400px] max-w-[calc(100vw-16px)] overflow-hidden rounded-xl border border-[#454540] bg-[#363632] font-sans text-[14px] shadow-[0_24px_48px_rgba(0,0,0,0.5)]"
          >
            <div className="flex items-center justify-between px-3.5 pb-2 pt-3">
              <span className="text-[15px] font-medium text-[#f4f0e8]">
                {t("notifications.title")}
              </span>
              <button
                type="button"
                disabled={unreadInList === 0}
                onClick={n.readAll}
                className="text-[13px] text-[#bdbab1] hover:text-white disabled:opacity-40"
              >
                {t("notifications.markAllRead")}
              </button>
            </div>
            <div className="flex gap-1 border-b border-[#454540] px-2.5 pb-2">
              {[false, true].map((unreadOnly) => (
                <button
                  key={String(unreadOnly)}
                  type="button"
                  onClick={() => n.setOnlyUnread(unreadOnly)}
                  className={`rounded-md px-2.5 py-0.5 text-[13px] ${n.onlyUnread === unreadOnly ? "bg-[#45453f] text-white" : "text-[#a8a59c] hover:text-white"}`}
                >
                  {unreadOnly
                    ? t("notifications.unread", { count: unreadInList })
                    : t("notifications.all")}
                </button>
              ))}
            </div>
            <div className="max-h-[470px] overflow-y-auto p-1">
              {n.loadError && (
                <div className="px-3 py-4 text-[13px] text-accent">
                  {t("notifications.loadFailed")}
                </div>
              )}
              {n.items !== null && shown.length === 0 && !n.loadError && (
                <div className="px-5 pb-8 pt-7 text-center">
                  <Icon name="bell" size="30px" className="text-[#5f5d57]" />
                  <div className="mt-2.5 text-[#d8d5cc]">
                    {t("notifications.empty")}
                  </div>
                  <div className="mt-0.5 text-[13px] text-[#807d74]">
                    {t("notifications.emptyHint")}
                  </div>
                </div>
              )}
              {shown.map((item) => (
                <NotificationItem
                  key={item.id}
                  notification={item}
                  onClick={() => n.openItem(item)}
                />
              ))}
            </div>
            <div className="flex items-center justify-between border-t border-[#454540] px-3.5 py-2 text-[13px]">
              <button
                type="button"
                className="flex items-center gap-1.5 text-[#bdbab1] hover:text-white"
                onClick={() => {
                  n.close();
                  onManageSchedules();
                }}
              >
                <Icon name="clock" size="13px" />
                {t("notifications.manage")}
              </button>
              <span className="text-[#7f7c74]">{t("notifications.kept")}</span>
            </div>
          </div>,
          document.body,
        )}
    </>
  );
}

// NotificationToast shows what a poll found. On a narrow screen it sits at the
// top, clear of the composer, since it stays until clicked away.
export function NotificationToast({
  notifications: n,
}: {
  notifications: Notifications;
}) {
  const { t } = useTranslation();
  const toast = n.toast;
  if (toast === null) return null;
  return createPortal(
    <div
      role="status"
      className="fixed inset-x-3 top-3 z-50 flex gap-3 rounded-xl border border-[#454540] bg-[#363632] px-3.5 py-3 font-sans text-[14px] shadow-[0_18px_40px_rgba(0,0,0,0.45)] md:inset-x-auto md:bottom-5 md:right-5 md:top-auto md:w-[360px]"
    >
      {toast.kind === "one" ? (
        <div className="min-w-0 flex-1">
          <NotificationText notification={toast.notification} />
          <div className="mt-2 flex gap-2">
            <button
              type="button"
              className="rounded-lg bg-white px-2.5 py-1 text-[13px] font-medium text-[#1d1d1b]"
              onClick={() => n.openItem(toast.notification)}
            >
              {t("notifications.open")}
            </button>
            <button
              type="button"
              className="rounded-lg px-2.5 py-1 text-[13px] text-[#d5d2c9] hover:bg-[#4a4a46]"
              onClick={n.dismissToast}
            >
              {t("notifications.dismiss")}
            </button>
          </div>
        </div>
      ) : (
        <div className="flex min-w-0 flex-1 items-center justify-between gap-2">
          <span className="text-[#ecece6]">
            {t("notifications.manyNew", { count: toast.count })}
          </span>
          <button
            type="button"
            className="rounded-lg bg-white px-2.5 py-1 text-[13px] font-medium text-[#1d1d1b]"
            onClick={() => {
              n.dismissToast();
              if (!n.open) n.toggle();
            }}
          >
            {t("notifications.openInbox")}
          </button>
        </div>
      )}
      <button
        type="button"
        aria-label={t("notifications.dismiss")}
        className="self-start text-[#8f8b82] hover:text-white"
        onClick={n.dismissToast}
      >
        <Icon name="close" size="15px" />
      </button>
    </div>,
    document.body,
  );
}

function NotificationText({ notification }: { notification: AppNotification }) {
  return (
    <div className="flex gap-2.5">
      <div
        className={`grid h-8 w-8 shrink-0 place-items-center rounded-lg ${notification.kind === "schedule_disabled" ? "bg-[rgba(217,83,79,0.16)] text-[#ec8f8b]" : "bg-[#2c2c29] text-[#d8d5cc]"}`}
      >
        <Icon
          name={
            notification.kind === "schedule_disabled" ? "alertCircle" : "clock"
          }
          size="17px"
        />
      </div>
      <div className="min-w-0 flex-1">
        <div className="font-medium leading-[19px] text-[#ecece6]">
          {notification.title}
        </div>
        {notification.body !== "" && (
          <div className="mt-0.5 line-clamp-2 text-[13px] leading-[18px] text-[#a29f96]">
            {notification.body}
          </div>
        )}
      </div>
    </div>
  );
}

function NotificationItem({
  notification,
  onClick,
}: {
  notification: AppNotification;
  onClick(): void;
}) {
  const unread = notification.readAt === null;
  return (
    <button
      type="button"
      onClick={onClick}
      className="relative flex w-full gap-2.5 rounded-lg px-2.5 py-2.5 text-left hover:bg-[#3f3f3a]"
    >
      <div className={`min-w-0 flex-1 ${unread ? "" : "opacity-80"}`}>
        <NotificationText notification={notification} />
        <div className="ml-[42px] mt-1 text-[12px] text-[#7f7c74]">
          {formatTimeAgo(notification.createdAt)}
        </div>
      </div>
      <span
        className={`mt-1.5 h-2 w-2 shrink-0 rounded-full bg-[#c9a25f] ${unread ? "" : "invisible"}`}
      />
    </button>
  );
}
