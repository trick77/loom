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
// TOAST_MS is how long a toast stays up.
export const TOAST_MS = 8_000;

type Toast =
  | { kind: "one"; notification: AppNotification }
  | { kind: "many"; count: number };

// NotificationBell is the sidebar's bell: a gold dot while anything is unread,
// the inbox in a popover, and a toast when a poll finds something new while
// the app is open.
export function NotificationBell({
  onOpen,
  onManageSchedules,
  placement,
}: {
  onOpen(notification: AppNotification): void;
  onManageSchedules(): void;
  placement: "header" | "rail";
}) {
  const { t } = useTranslation();
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<AppNotification[] | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [onlyUnread, setOnlyUnread] = useState(false);
  const [toast, setToast] = useState<Toast | null>(null);
  const [panelStyle, setPanelStyle] = useState({ top: 48, left: 16 });
  const known = useRef<number | null>(null);
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);

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
    if (before !== null && count > before) {
      // The sidebar picks up the new run threads and their dots.
      announce(THREAD_NEWS_CHANGED);
      const fresh = count - before;
      const list = await load();
      const newest = list?.find((n) => n.readAt === null);
      setToast(
        fresh > 1 || !newest
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
    if (toast === null) return;
    const handle = window.setTimeout(() => setToast(null), TOAST_MS);
    return () => window.clearTimeout(handle);
  }, [toast]);

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: PointerEvent) => {
      const target = event.target as Node;
      if (panel.current?.contains(target) || button.current?.contains(target))
        return;
      setOpen(false);
    };
    document.addEventListener("pointerdown", onPointer);
    return () => document.removeEventListener("pointerdown", onPointer);
  }, [open]);
  useEscapeKey(() => setOpen(false), { active: open });

  const toggle = () => {
    if (!open) {
      const rect = button.current?.getBoundingClientRect();
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

  const shown = (items ?? []).filter((n) => !onlyUnread || n.readAt === null);
  const unreadInList = (items ?? []).filter((n) => n.readAt === null).length;

  return (
    <>
      <button
        ref={button}
        type="button"
        aria-label={t(
          unread > 0 ? "notifications.bellUnread" : "notifications.bell",
        )}
        aria-expanded={open}
        onClick={toggle}
        className={`relative grid place-items-center rounded transition-colors hover:text-white ${open ? "text-white" : ""}`}
      >
        <Icon name="bell" size={placement === "header" ? "18px" : "20px"} />
        {unread > 0 && (
          <span
            data-testid="notification-dot"
            className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full bg-[#c9a25f] ring-2 ring-[#1b1b1a]"
          />
        )}
      </button>
      {open &&
        createPortal(
          <div
            ref={panel}
            role="dialog"
            aria-label={t("notifications.title")}
            style={panelStyle}
            className="fixed z-50 w-[400px] max-w-[calc(100vw-16px)] overflow-hidden rounded-xl border border-[#454540] bg-[#363632] font-sans text-[14px] shadow-[0_24px_48px_rgba(0,0,0,0.5)]"
          >
            <div className="flex items-center justify-between px-3.5 pb-2 pt-3">
              <span className="text-[15px] font-medium text-[#f4f0e8]">
                {t("notifications.title")}
              </span>
              <button
                type="button"
                disabled={unreadInList === 0}
                onClick={readAll}
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
                  onClick={() => setOnlyUnread(unreadOnly)}
                  className={`rounded-md px-2.5 py-0.5 text-[13px] ${onlyUnread === unreadOnly ? "bg-[#45453f] text-white" : "text-[#a8a59c] hover:text-white"}`}
                >
                  {unreadOnly
                    ? t("notifications.unread", { count: unreadInList })
                    : t("notifications.all")}
                </button>
              ))}
            </div>
            <div className="max-h-[470px] overflow-y-auto p-1">
              {loadError && (
                <div className="px-3 py-4 text-[13px] text-accent">
                  {t("notifications.loadFailed")}
                </div>
              )}
              {items !== null && shown.length === 0 && !loadError && (
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
              {shown.map((n) => (
                <NotificationItem
                  key={n.id}
                  notification={n}
                  onClick={() => openItem(n)}
                />
              ))}
            </div>
            <div className="flex items-center justify-between border-t border-[#454540] px-3.5 py-2 text-[13px]">
              <button
                type="button"
                className="flex items-center gap-1.5 text-[#bdbab1] hover:text-white"
                onClick={() => {
                  setOpen(false);
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
      {toast !== null &&
        createPortal(
          <div
            role="status"
            className="fixed bottom-5 right-5 z-50 flex w-[360px] max-w-[calc(100vw-24px)] gap-3 rounded-xl border border-[#454540] bg-[#363632] px-3.5 py-3 font-sans text-[14px] shadow-[0_18px_40px_rgba(0,0,0,0.45)]"
          >
            {toast.kind === "one" ? (
              <div className="min-w-0 flex-1">
                <NotificationText notification={toast.notification} />
                <div className="mt-2 flex gap-2">
                  <button
                    type="button"
                    className="rounded-lg bg-white px-2.5 py-1 text-[13px] font-medium text-[#1d1d1b]"
                    onClick={() => openItem(toast.notification)}
                  >
                    {t("notifications.open")}
                  </button>
                  <button
                    type="button"
                    className="rounded-lg px-2.5 py-1 text-[13px] text-[#d5d2c9] hover:bg-[#4a4a46]"
                    onClick={() => setToast(null)}
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
                    setToast(null);
                    if (!open) toggle();
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
              onClick={() => setToast(null)}
            >
              <Icon name="close" size="15px" />
            </button>
          </div>,
          document.body,
        )}
    </>
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
