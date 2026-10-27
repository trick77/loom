import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  deleteSchedule,
  listSchedules,
  runScheduleNow,
  updateSchedule,
  type Schedule,
} from "../api";
import { describeActionError } from "../chat/actionErrors";
import { ErrorText } from "../chat/ErrorText";
import { Icon } from "../chat/Icon";
import { ModalShell } from "../chat/threadModals";
import { useOutsideRefPointerDown } from "../chat/useOutsidePointerDown";
import { SidebarOpenButton } from "../SidebarOpenButton";
import {
  menuDeleteItemClass,
  menuIconClass,
  menuItemClass,
  modalCancelButtonClass,
  modalDangerButtonClass,
} from "../ThreadActionsMenu";
import {
  ScheduleEditDialog,
  scheduleStatus,
  StatusPill,
  Toggle,
} from "./ScheduleParts";
import {
  formatDay,
  formatRunDate,
  RUN_MINUTES,
  slotText,
} from "./scheduleText";

type Tab = "all" | "active" | "paused";

// RUNNING_POLL_MS is how often a page reloads while a schedule runs.
export const RUNNING_POLL_MS = 5000;

// useScheduleActions are the actions the list and the detail page share.
export function useScheduleActions(
  onChanged: (schedule: Schedule) => void,
  onSessionExpired: () => void,
) {
  const { t } = useTranslation();
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const fail = useCallback(
    (err: unknown) => {
      const message = describeActionError(err, t("schedules.actionFailed"));
      if (message === null) onSessionExpired();
      else setError(message);
    },
    [t, onSessionExpired],
  );
  const setEnabled = useCallback(
    async (schedule: Schedule, enabled: boolean) => {
      setBusy(true);
      setError("");
      try {
        onChanged(await updateSchedule(schedule.id, { enabled }));
      } catch (err) {
        fail(err);
      } finally {
        setBusy(false);
      }
    },
    [onChanged, fail],
  );
  const runNow = useCallback(
    async (schedule: Schedule) => {
      setError("");
      setNotice("");
      try {
        await runScheduleNow(schedule.id);
        setNotice(t("schedules.runStarted"));
        onChanged({ ...schedule, running: true });
      } catch (err) {
        fail(err);
      }
    },
    [onChanged, fail, t],
  );
  return { notice, error, busy, setEnabled, runNow, fail };
}

export function ScheduleMenu({
  schedule,
  onRunNow,
  onEdit,
  onOpenThread,
  onDelete,
}: {
  schedule: Schedule;
  onRunNow(): void;
  onEdit(): void;
  onOpenThread(threadID: string): void;
  onDelete(): void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useOutsideRefPointerDown(open, ref, () => setOpen(false));
  const item = (
    label: string,
    icon: Parameters<typeof Icon>[0]["name"],
    onClick: () => void,
    danger = false,
  ) => (
    <button
      type="button"
      role="menuitem"
      className={`${danger ? menuDeleteItemClass : menuItemClass} ${danger ? "" : "text-[#f3f0e8]"}`}
      onClick={() => {
        setOpen(false);
        onClick();
      }}
    >
      <Icon name={icon} size="16px" className={menuIconClass} />
      {label}
    </button>
  );
  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-label={t("schedules.menu")}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="grid h-7 w-7 place-items-center rounded-md text-[#a8a59c] hover:bg-[#3a3a37] hover:text-white"
      >
        <Icon name="moreVertical" size="18px" />
      </button>
      {open && (
        <div
          role="menu"
          className="ui-sidebar-text absolute right-0 top-full z-20 mt-1 w-[200px] rounded-[10px] border border-[#454540] bg-[#363632] p-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)]"
        >
          {item(t("schedules.runNow"), "play", onRunNow)}
          {item(t("schedules.edit"), "edit", onEdit)}
          {schedule.lastRun?.threadId != null &&
            item(t("schedules.openLatestRun"), "messages", () =>
              onOpenThread(schedule.lastRun?.threadId as string),
            )}
          <div className="mx-0.5 my-1 h-px bg-[#454540]" />
          {item(t("common.delete"), "trash", onDelete, true)}
        </div>
      )}
    </div>
  );
}

export function DeleteScheduleModal({
  onCancel,
  onDelete,
  error,
}: {
  onCancel(): void;
  onDelete(): void;
  error: string;
}) {
  const { t } = useTranslation();
  return (
    <ModalShell title={t("schedules.deleteTitle")} onCancel={onCancel}>
      <div className="mt-3 text-sm leading-6 text-[#d8d4ca]">
        {t("schedules.deleteConfirm")}
      </div>
      {error !== "" && <ErrorText>{error}</ErrorText>}
      <div className="mt-4 flex justify-end gap-2">
        <button
          autoFocus
          type="button"
          className={modalCancelButtonClass}
          onClick={onCancel}
        >
          {t("common.cancel")}
        </button>
        <button
          type="button"
          className={modalDangerButtonClass}
          onClick={onDelete}
        >
          {t("common.delete")}
        </button>
      </div>
    </ModalShell>
  );
}

// ScheduleSlotLine is "“once a week” → Weekly on Thursday at 14:37 · next …".
export function ScheduleSlotLine({ schedule }: { schedule: Schedule }) {
  const { t, i18n } = useTranslation();
  let tail: string;
  if (schedule.disabledReason === "timeout") {
    tail = t("schedules.disabledNote", {
      date: schedule.disabledAt
        ? formatDay(schedule.disabledAt, i18n.language)
        : "",
      minutes: RUN_MINUTES,
    });
  } else {
    const slot = slotText(schedule.cron, schedule.summary, i18n.language, t);
    const when = schedule.enabled
      ? schedule.nextRuns.length > 0
        ? t("schedules.next", {
            date: formatRunDate(
              schedule.nextRuns[0],
              i18n.language,
              schedule.timezone,
            ),
          })
        : ""
      : t("schedules.pausedNote");
    tail = when === "" ? slot : `${slot} · ${when}`;
  }
  return (
    <div className="mt-1.5 flex flex-wrap items-center gap-1.5 text-[13px] text-[#8a887f]">
      <q className="text-[#b9b6ad]">{schedule.interval}</q>
      <span className="text-[#5f5d57]">→</span>
      <span>{tail}</span>
    </div>
  );
}

export function SchedulesPage({
  onOpenSidebar,
  onOpenSchedule,
  onOpenThread,
  onSessionExpired,
}: {
  onOpenSidebar(): void;
  onOpenSchedule(scheduleID: string): void;
  onOpenThread(threadID: string): void;
  onSessionExpired(): void;
}) {
  const { t } = useTranslation();
  const [schedules, setSchedules] = useState<Schedule[] | null>(null);
  const [loadError, setLoadError] = useState("");
  const [tab, setTab] = useState<Tab>("all");
  const [editing, setEditing] = useState<Schedule | "new" | null>(null);
  const [deleting, setDeleting] = useState<Schedule | null>(null);
  const [deleteError, setDeleteError] = useState("");

  const replace = useCallback((updated: Schedule) => {
    setSchedules((current) => {
      if (current === null) return [updated];
      const found = current.some((s) => s.id === updated.id);
      return found
        ? current.map((s) =>
            s.id === updated.id
              ? { ...updated, lastRun: updated.lastRun ?? s.lastRun }
              : s,
          )
        : [...current, updated];
    });
  }, []);
  const actions = useScheduleActions(replace, onSessionExpired);

  const [reload, setReload] = useState(0);
  useEffect(() => {
    let cancelled = false;
    listSchedules().then(
      (list) => {
        if (!cancelled) setSchedules(list);
      },
      (err: unknown) => {
        if (cancelled) return;
        const message = describeActionError(err, t("schedules.loadFailed"));
        if (message === null) onSessionExpired();
        else setLoadError(message);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [t, onSessionExpired, reload]);

  // While a schedule runs, the list reloads until its run lands.
  const anyRunning = (schedules ?? []).some((s) => s.running);
  useEffect(() => {
    if (!anyRunning) return;
    const handle = window.setTimeout(
      () => setReload((n) => n + 1),
      RUNNING_POLL_MS,
    );
    return () => window.clearTimeout(handle);
  }, [anyRunning, reload]);

  const all = schedules ?? [];
  const active = all.filter((s) => s.enabled);
  const shown =
    tab === "active"
      ? active
      : tab === "paused"
        ? all.filter((s) => !s.enabled)
        : all;

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="relative mx-auto w-full max-w-[802px] px-4 pb-16 pt-10 md:px-6">
        <SidebarOpenButton variant="floating" onClick={onOpenSidebar} />
        <header className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">
            {t("schedules.title")}
          </h1>
          <button
            type="button"
            onClick={() => setEditing("new")}
            className="ui-control-text flex items-center gap-1.5 rounded-lg bg-white px-3 py-1.5 font-medium text-[#1d1d1b]"
          >
            <Icon name="plus" size="14px" />
            {t("schedules.newSchedule")}
          </button>
        </header>
        <p className="mt-2 text-[14px] text-[#8f8b82]">
          {t("schedules.subtitle")}
        </p>
        {loadError !== "" && <ErrorText>{loadError}</ErrorText>}
        {actions.error !== "" && <ErrorText>{actions.error}</ErrorText>}
        {actions.notice !== "" && (
          <div className="ui-meta-text mt-3 rounded-lg border border-[#3a3a37] bg-[#282826] px-4 py-2.5 text-[#c7c5bd]">
            {actions.notice}
          </div>
        )}
        {schedules !== null && schedules.length === 0 && (
          <div className="py-14 text-center">
            <Icon name="clock" size="34px" className="text-[#5f5d57]" />
            <div className="mt-3 text-[#d8d5cc]">{t("schedules.empty")}</div>
            <div className="mx-auto mt-1 max-w-[420px] text-[14px] text-[#807d74]">
              {t("schedules.emptyHint")}
            </div>
          </div>
        )}
        {all.length > 0 && (
          <>
            <div className="mt-5 flex items-center gap-1" role="tablist">
              {(["all", "active", "paused"] as const).map((option) => (
                <button
                  key={option}
                  type="button"
                  role="tab"
                  aria-selected={tab === option}
                  onClick={() => setTab(option)}
                  className={`ui-control-text rounded-lg px-3 py-1 ${tab === option ? "bg-[#3a3a37] text-white" : "text-[#a8a59c] hover:text-white"}`}
                >
                  {t(
                    option === "all"
                      ? "schedules.tabAll"
                      : option === "active"
                        ? "schedules.tabActive"
                        : "schedules.tabPaused",
                    {
                      count:
                        option === "all"
                          ? all.length
                          : option === "active"
                            ? active.length
                            : all.length - active.length,
                    },
                  )}
                </button>
              ))}
            </div>
            <ul className="mt-3">
              {shown.map((schedule) => {
                const status = scheduleStatus(schedule);
                return (
                  <li
                    key={schedule.id}
                    className="flex items-start gap-3.5 rounded-[10px] border-t border-[#2a2a28] px-3 py-3.5 first:border-t-0 hover:bg-[#2a2a28]"
                  >
                    <div className="mt-0.5 grid h-[34px] w-[34px] shrink-0 place-items-center rounded-[9px] bg-[#2a2a28] text-[#d8d5cc]">
                      <Icon name="clock" size="18px" />
                    </div>
                    <button
                      type="button"
                      className={`min-w-0 flex-1 text-left ${schedule.enabled ? "" : "opacity-75"}`}
                      onClick={() => onOpenSchedule(schedule.id)}
                    >
                      <div className="flex items-center gap-2 text-[15px] text-[#ecece6]">
                        <span className="truncate">{schedule.name}</span>
                        {status !== null && <StatusPill status={status} />}
                      </div>
                      <div className="mt-0.5 truncate text-[13px] text-[#908e85]">
                        {schedule.prompt}
                      </div>
                      <ScheduleSlotLine schedule={schedule} />
                    </button>
                    <div className="flex items-center gap-2.5 pt-1.5">
                      <Toggle
                        on={schedule.enabled}
                        disabled={actions.busy}
                        label={t(
                          schedule.enabled
                            ? "schedules.pause"
                            : "schedules.resume",
                        )}
                        onChange={(on) => void actions.setEnabled(schedule, on)}
                      />
                      <ScheduleMenu
                        schedule={schedule}
                        onRunNow={() => void actions.runNow(schedule)}
                        onEdit={() => setEditing(schedule)}
                        onOpenThread={onOpenThread}
                        onDelete={() => {
                          setDeleteError("");
                          setDeleting(schedule);
                        }}
                      />
                    </div>
                  </li>
                );
              })}
            </ul>
          </>
        )}
      </div>
      {editing !== null && (
        <ScheduleEditDialog
          schedule={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={(saved) => {
            replace(saved);
            setEditing(null);
          }}
          onSessionExpired={onSessionExpired}
        />
      )}
      {deleting !== null && (
        <DeleteScheduleModal
          error={deleteError}
          onCancel={() => setDeleting(null)}
          onDelete={() => {
            deleteSchedule(deleting.id).then(
              () => {
                setSchedules((current) =>
                  (current ?? []).filter((s) => s.id !== deleting.id),
                );
                setDeleting(null);
              },
              (err: unknown) => {
                const message = describeActionError(
                  err,
                  t("schedules.actionFailed"),
                );
                if (message === null) onSessionExpired();
                else setDeleteError(message);
              },
            );
          }}
        />
      )}
    </div>
  );
}
