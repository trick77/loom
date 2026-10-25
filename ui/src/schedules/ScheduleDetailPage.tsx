import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  deleteSchedule,
  forgetScheduleMemory,
  getSchedule,
  getScheduleMemory,
  listScheduleRuns,
  type Schedule,
  type ScheduleMemory,
  type ScheduleMemoryKind,
  type ScheduleRun,
} from "../api";
import { describeActionError } from "../chat/actionErrors";
import { ErrorText } from "../chat/ErrorText";
import { Icon } from "../chat/Icon";
import { SidebarOpenButton } from "../SidebarOpenButton";
import {
  DisabledBanner,
  ScheduleEditDialog,
  StatusPill,
  Toggle,
} from "./ScheduleParts";
import {
  DeleteScheduleModal,
  RUNNING_POLL_MS,
  ScheduleSlotLine,
  useScheduleActions,
} from "./SchedulesPage";
import {
  formatDay,
  formatDuration,
  formatRunDate,
  formatTokens,
} from "./scheduleText";

const secondaryButton =
  "ui-control-text flex items-center gap-1.5 rounded-lg bg-[#3a3a37] px-3 py-1.5 text-[#d5d2c9] hover:bg-[#4a4a46] disabled:opacity-50";

const REPORTED_PREVIEW = 4;

export function ScheduleDetailPage({
  scheduleID,
  onOpenSidebar,
  onBack,
  onOpenThread,
  onSessionExpired,
}: {
  scheduleID: string;
  onOpenSidebar(): void;
  onBack(): void;
  onOpenThread(threadID: string): void;
  onSessionExpired(): void;
}) {
  const { t, i18n } = useTranslation();
  const [schedule, setSchedule] = useState<Schedule | null>(null);
  const [runs, setRuns] = useState<ScheduleRun[]>([]);
  const [memory, setMemory] = useState<ScheduleMemory | null>(null);
  const [loadError, setLoadError] = useState("");
  const [tab, setTab] = useState<"runs" | "memory">("runs");
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [showAllReported, setShowAllReported] = useState(false);
  const [reload, setReload] = useState(0);

  const onChanged = useCallback(
    (updated: Schedule) => setSchedule(updated),
    [],
  );
  const actions = useScheduleActions(onChanged, onSessionExpired);

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      getSchedule(scheduleID),
      listScheduleRuns(scheduleID),
      getScheduleMemory(scheduleID),
    ]).then(
      ([loaded, loadedRuns, loadedMemory]) => {
        if (cancelled) return;
        setSchedule(loaded);
        setRuns(loadedRuns);
        setMemory(loadedMemory);
      },
      (err: unknown) => {
        if (cancelled) return;
        const message = describeActionError(
          err,
          t("schedules.detail.notFound"),
        );
        if (message === null) onSessionExpired();
        else setLoadError(message);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [scheduleID, reload, t, onSessionExpired]);

  // A running schedule is polled until its run lands in the history.
  const running = schedule?.running === true;
  useEffect(() => {
    if (!running) return;
    const handle = window.setTimeout(
      () => setReload((n) => n + 1),
      RUNNING_POLL_MS,
    );
    return () => window.clearTimeout(handle);
  }, [running, reload]);

  const forget = (kind: ScheduleMemoryKind, key?: string) => {
    forgetScheduleMemory(scheduleID, kind, key).then(
      () => setReload((n) => n + 1),
      actions.fail,
    );
  };

  if (schedule === null) {
    return (
      <div className="flex h-full flex-col overflow-y-auto">
        <div className="relative mx-auto w-full max-w-[802px] px-4 pb-16 pt-10 md:px-6">
          <SidebarOpenButton variant="floating" onClick={onOpenSidebar} />
          {loadError !== "" && <ErrorText>{loadError}</ErrorText>}
        </div>
      </div>
    );
  }

  const findings = memory?.findings ?? [];
  const sources = memory?.sources ?? [];
  const queries = memory?.queries ?? [];
  const memoryCount = findings.length + sources.length + queries.length;
  const reported = showAllReported
    ? findings
    : findings.slice(0, REPORTED_PREVIEW);

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <div className="relative mx-auto w-full max-w-[802px] px-4 pb-16 pt-8 md:px-6">
        <SidebarOpenButton variant="floating" onClick={onOpenSidebar} />
        <button
          type="button"
          onClick={onBack}
          className="ui-meta-text flex items-center gap-1.5 text-[#8f8b82] hover:text-white"
        >
          {t("schedules.title")} <Icon name="chevronRight" size="12px" />
        </button>
        <header className="mt-1.5 flex flex-wrap items-center justify-between gap-2">
          <h1 className="font-serif text-[28px] font-medium leading-8 text-[#f4f0e8]">
            {schedule.name}
          </h1>
          <div className="flex flex-wrap items-center gap-2">
            <Toggle
              on={schedule.enabled}
              disabled={actions.busy}
              label={t(
                schedule.enabled ? "schedules.pause" : "schedules.resume",
              )}
              onChange={(on) => void actions.setEnabled(schedule, on)}
            />
            <button
              type="button"
              className={secondaryButton}
              disabled={schedule.running}
              onClick={() => void actions.runNow(schedule)}
            >
              <Icon name="play" size="14px" />
              {t("schedules.runNow")}
            </button>
            <button
              type="button"
              className={secondaryButton}
              onClick={() => setEditing(true)}
            >
              <Icon name="edit" size="14px" />
              {t("schedules.edit")}
            </button>
            {schedule.threadId !== null && (
              <button
                type="button"
                className={secondaryButton}
                onClick={() => onOpenThread(schedule.threadId as string)}
              >
                <Icon name="messages" size="14px" />
                {t("schedules.detail.thread")}
              </button>
            )}
            <button
              type="button"
              aria-label={t("common.delete")}
              className="grid h-8 w-8 place-items-center rounded-lg text-[#a8a59c] hover:bg-[#3a3a37] hover:text-[#ec7e7e]"
              onClick={() => {
                setDeleteError("");
                setDeleting(true);
              }}
            >
              <Icon name="trash" size="16px" />
            </button>
          </div>
        </header>
        {actions.error !== "" && <ErrorText>{actions.error}</ErrorText>}
        {actions.notice !== "" && (
          <div className="ui-meta-text mt-3 rounded-lg border border-[#3a3a37] bg-[#282826] px-4 py-2.5 text-[#c7c5bd]">
            {actions.notice}
          </div>
        )}
        <div className="mt-4">
          <DisabledBanner
            schedule={schedule}
            busy={actions.busy}
            onEnable={() => void actions.setEnabled(schedule, true)}
          />
        </div>
        <ScheduleSlotLine schedule={schedule} />
        <dl className="mt-4 grid grid-cols-[110px_1fr] gap-x-4 gap-y-2 text-[14px]">
          <dt className="text-[#8f8b82]">{t("schedules.detail.nextRun")}</dt>
          <dd className="text-[#d8d5cc]">
            {schedule.enabled && schedule.nextRuns.length > 0
              ? `${formatRunDate(schedule.nextRuns[0], i18n.language, schedule.timezone)} (${schedule.timezone})`
              : t("schedules.detail.none")}
          </dd>
          <dt className="text-[#8f8b82]">{t("schedules.detail.memory")}</dt>
          <dd className="text-[#d8d5cc]">
            {t("schedules.detail.memorySummary", {
              findings: findings.length,
              sources: sources.length,
              queries: queries.length,
            })}
          </dd>
        </dl>
        <div className="mt-3.5 whitespace-pre-wrap rounded-[10px] border border-[#343432] bg-[#181817] px-3.5 py-3 text-[14px] text-[#d8d5cc]">
          {schedule.prompt}
        </div>
        <div className="mt-7 flex items-center gap-1" role="tablist">
          {(["runs", "memory"] as const).map((option) => (
            <button
              key={option}
              type="button"
              role="tab"
              aria-selected={tab === option}
              onClick={() => setTab(option)}
              className={`ui-control-text rounded-lg px-3 py-1 ${tab === option ? "bg-[#3a3a37] text-white" : "text-[#a8a59c] hover:text-white"}`}
            >
              {option === "runs"
                ? t("schedules.detail.runsTab", { count: runs.length })
                : t("schedules.detail.memoryTab", { count: memoryCount })}
            </button>
          ))}
        </div>
        {tab === "runs" ? (
          runs.length === 0 ? (
            <div className="py-8 text-center text-[14px] text-[#807d74]">
              {t("schedules.detail.noRuns")}
            </div>
          ) : (
            <div className="mt-2 overflow-x-auto">
              <table className="w-full border-collapse text-[14px]">
                <thead>
                  <tr className="text-left text-[12px] text-[#8f8b82]">
                    <th className="border-b border-[#343432] px-2 py-1.5 font-normal">
                      {t("schedules.detail.colWhen")}
                    </th>
                    <th className="border-b border-[#343432] px-2 py-1.5 font-normal">
                      {t("schedules.detail.colResult")}
                    </th>
                    <th className="border-b border-[#343432] px-2 py-1.5 font-normal">
                      {t("schedules.detail.colTokens")}
                    </th>
                    <th className="border-b border-[#343432] px-2 py-1.5 font-normal">
                      {t("schedules.detail.colDuration")}
                    </th>
                    <th className="border-b border-[#343432] px-2 py-1.5" />
                  </tr>
                </thead>
                <tbody>
                  {runs.map((run) => (
                    <tr key={run.id} className="text-[#d8d5cc]">
                      <td className="whitespace-nowrap border-b border-[#2a2a28] px-2 py-2.5">
                        {formatRunDate(
                          run.startedAt,
                          i18n.language,
                          schedule.timezone,
                        )}
                      </td>
                      <td className="border-b border-[#2a2a28] px-2 py-2.5">
                        <span className="flex flex-wrap items-center gap-1.5">
                          <StatusPill
                            status={run.status}
                            count={run.newCount}
                          />
                          <span className="text-[13px] text-[#908e85]">
                            {run.newLabels.length > 0
                              ? run.newLabels.join(", ")
                              : run.error}
                          </span>
                        </span>
                      </td>
                      <td className="border-b border-[#2a2a28] px-2 py-2.5 tabular-nums text-[#a8a59c]">
                        {formatTokens(run.tokens)}
                      </td>
                      <td className="border-b border-[#2a2a28] px-2 py-2.5 tabular-nums text-[#a8a59c]">
                        {formatDuration(run.durationMs)}
                      </td>
                      <td className="whitespace-nowrap border-b border-[#2a2a28] px-2 py-2.5">
                        {run.messageId !== null &&
                          schedule.threadId !== null && (
                            <button
                              type="button"
                              className="text-[13px] text-accent-strong hover:underline"
                              onClick={() =>
                                onOpenThread(schedule.threadId as string)
                              }
                            >
                              {t("schedules.detail.openTurn")}
                            </button>
                          )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        ) : (
          <div>
            {memoryCount === 0 && (
              <div className="py-8 text-center text-[14px] text-[#807d74]">
                {t("schedules.memory.empty")}
              </div>
            )}
            {findings.length > 0 && (
              <MemorySection
                title={t("schedules.memory.reported")}
                hint={t("schedules.memory.reportedHint")}
                onForgetAll={() => forget("findings")}
              >
                {reported.map((f) => (
                  <MemoryRow
                    key={f.key}
                    onForget={() => forget("findings", f.key)}
                  >
                    <span className="min-w-0 flex-1 truncate text-[#ecece6]">
                      {f.label}
                    </span>
                    <span className="whitespace-nowrap text-[13px] text-[#8a887f]">
                      {t("schedules.memory.reportedOn", {
                        date: formatDay(f.firstSeenAt, i18n.language),
                      })}
                    </span>
                  </MemoryRow>
                ))}
                {findings.length > REPORTED_PREVIEW && !showAllReported && (
                  <button
                    type="button"
                    className="w-full border-t border-[#2a2a28] px-3.5 py-2.5 text-center text-[13px] text-[#a8a59c] hover:text-white"
                    onClick={() => setShowAllReported(true)}
                  >
                    {t("schedules.memory.showAll", { count: findings.length })}
                  </button>
                )}
              </MemorySection>
            )}
            {sources.length > 0 && (
              <MemorySection
                title={t("schedules.memory.sources")}
                hint={t("schedules.memory.sourcesHint")}
                onForgetAll={() => forget("sources")}
              >
                {sources.map((s, i) => (
                  <MemoryRow
                    key={s.url}
                    onForget={() => forget("sources", s.url)}
                  >
                    <span className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
                      <span className="truncate font-mono text-[13px] text-[#d8d5cc]">
                        {s.url}
                      </span>
                      {i === 0 && s.finds > 0 ? (
                        <Chip tone="new">
                          {t("schedules.memory.topSource")}
                        </Chip>
                      ) : s.finds > 0 ? (
                        <Chip tone="new">
                          {t("schedules.memory.finds", { count: s.finds })}
                        </Chip>
                      ) : (
                        <Chip tone="none">{t("schedules.memory.noFinds")}</Chip>
                      )}
                      {s.prefer === "browser" && (
                        <Chip tone="limit">
                          {t("schedules.memory.useBrowser")}
                        </Chip>
                      )}
                    </span>
                    <span className="whitespace-nowrap text-[13px] text-[#8a887f]">
                      {[
                        i === 0 && s.finds > 0
                          ? t("schedules.memory.finds", { count: s.finds })
                          : "",
                        s.fetchFailedAt && s.prefer === "browser"
                          ? t("schedules.memory.fetchBlocked", {
                              date: formatDay(s.fetchFailedAt, i18n.language),
                            })
                          : s.runs > 0
                            ? t("schedules.memory.runs", { count: s.runs })
                            : "",
                        t("schedules.memory.lastUsed", {
                          date: formatDay(s.lastUsedAt, i18n.language),
                        }),
                      ]
                        .filter((part) => part !== "")
                        .join(" · ")}
                    </span>
                  </MemoryRow>
                ))}
              </MemorySection>
            )}
            {queries.length > 0 && (
              <MemorySection
                title={t("schedules.memory.queries")}
                hint={t("schedules.memory.queriesHint")}
                onForgetAll={() => forget("queries")}
              >
                {queries.map((q) => (
                  <MemoryRow
                    key={q.query}
                    onForget={() => forget("queries", q.query)}
                  >
                    <span className="flex min-w-0 flex-1 items-center gap-2">
                      <Icon
                        name="search"
                        size="14px"
                        className="shrink-0 text-[#8f8b82]"
                      />
                      <span className="truncate font-mono text-[13px] text-[#d8d5cc]">
                        {q.query}
                      </span>
                      {q.finds > 0 ? (
                        <Chip tone="new">
                          {t("schedules.memory.finds", { count: q.finds })}
                        </Chip>
                      ) : (
                        <Chip tone="none">
                          {t("schedules.memory.emptyRunsLeft", {
                            count: q.emptyRuns,
                          })}
                        </Chip>
                      )}
                    </span>
                    <span className="whitespace-nowrap text-[13px] text-[#8a887f]">
                      {t("schedules.memory.runs", { count: q.runs })} ·{" "}
                      {t("schedules.memory.lastUsed", {
                        date: formatDay(q.lastUsedAt, i18n.language),
                      })}
                    </span>
                  </MemoryRow>
                ))}
              </MemorySection>
            )}
          </div>
        )}
      </div>
      {editing && (
        <ScheduleEditDialog
          schedule={schedule}
          onClose={() => setEditing(false)}
          onSaved={(saved) => {
            setSchedule(saved);
            setEditing(false);
          }}
          onSessionExpired={onSessionExpired}
        />
      )}
      {deleting && (
        <DeleteScheduleModal
          error={deleteError}
          onCancel={() => setDeleting(false)}
          onDelete={() => {
            deleteSchedule(schedule.id).then(onBack, (err: unknown) => {
              const message = describeActionError(
                err,
                t("schedules.actionFailed"),
              );
              if (message === null) onSessionExpired();
              else setDeleteError(message);
            });
          }}
        />
      )}
    </div>
  );
}

function MemorySection({
  title,
  hint,
  onForgetAll,
  children,
}: {
  title: string;
  hint: string;
  onForgetAll(): void;
  children: React.ReactNode;
}) {
  const { t } = useTranslation();
  return (
    <section className="mt-6">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h3 className="font-serif text-[18px] font-medium leading-6 text-[#f4f0e8]">
            {title}
          </h3>
          <p className="mt-0.5 text-[13px] text-[#8f8b82]">{hint}</p>
        </div>
        <button
          type="button"
          className="whitespace-nowrap text-[13px] text-[#a8a59c] hover:text-white"
          onClick={onForgetAll}
        >
          {t("schedules.memory.forgetAll")}
        </button>
      </div>
      <div className="mt-2.5 rounded-[10px] border border-[#343432] bg-[#181817]">
        {children}
      </div>
    </section>
  );
}

function MemoryRow({
  children,
  onForget,
}: {
  children: React.ReactNode;
  onForget(): void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex items-center gap-3 border-t border-[#2a2a28] px-3.5 py-2.5 text-[14px] first:border-t-0">
      {children}
      <button
        type="button"
        aria-label={t("schedules.memory.forget")}
        className="text-[#6f6d66] hover:text-white"
        onClick={onForget}
      >
        <Icon name="close" size="15px" />
      </button>
    </div>
  );
}

function Chip({
  tone,
  children,
}: {
  tone: "new" | "none" | "limit";
  children: React.ReactNode;
}) {
  const tones = {
    new: "bg-[rgba(95,169,122,0.14)] text-[#8fd0a6]",
    none: "bg-[#2e2e2c] text-[#a8a59c]",
    limit: "bg-[rgba(201,162,95,0.14)] text-[#dcbc84]",
  };
  return (
    <span
      className={`whitespace-nowrap rounded-full px-2 py-px text-[12px] ${tones[tone]}`}
    >
      {children}
    </span>
  );
}
