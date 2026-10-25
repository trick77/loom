import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  createSchedule,
  previewSchedule,
  ScheduleIntervalError,
  updateSchedule,
  type Schedule,
  type SchedulePreview,
  type ScheduleRunStatus,
} from "../api";
import { describeActionError } from "../chat/actionErrors";
import { ErrorText } from "../chat/ErrorText";
import { Icon, type IconName } from "../chat/Icon";
import { useEscapeKey } from "../chat/useEscapeKey";
import { modalCancelButtonClass } from "../ThreadActionsMenu";
import { useDebouncedValue } from "../useDebouncedValue";
import {
  formatDay,
  formatRunDate,
  formatTokens,
  RUN_MINUTES,
  RUN_TOKEN_LIMIT,
  slotText,
} from "./scheduleText";

const pillTone: Record<string, string> = {
  new: "bg-[rgba(95,169,122,0.14)] text-[#8fd0a6]",
  nothing_new: "bg-[#2e2e2c] text-[#a8a59c]",
  token_limit: "bg-[rgba(201,162,95,0.14)] text-[#dcbc84]",
  failed: "bg-[rgba(184,92,82,0.18)] text-[#e59a90]",
  timeout: "bg-[rgba(217,83,79,0.14)] text-[#ec8f8b]",
  running: "bg-[#363632] text-[#d6d3ca]",
  paused: "bg-[#2e2e2c] text-[#8f8b82]",
};

const pillIcon: Partial<Record<string, IconName>> = {
  new: "checkCircle",
  token_limit: "warning",
  failed: "alertCircle",
  timeout: "alertCircle",
  paused: "pause",
};

// StatusPill names a run's outcome, or the schedule's state when that says
// more (paused, auto-disabled).
export function StatusPill({
  status,
  count = 0,
}: {
  status: ScheduleRunStatus | "paused";
  count?: number;
}) {
  const { t } = useTranslation();
  const icon = pillIcon[status];
  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1 whitespace-nowrap rounded-full px-2 py-px text-[12px] leading-[18px] ${pillTone[status] ?? pillTone.nothing_new}`}
    >
      {icon && <Icon name={icon} size="13px" />}
      {t(`schedules.status.${status}`, { count })}
    </span>
  );
}

// scheduleStatus is what a schedule's pill shows.
export function scheduleStatus(
  schedule: Schedule,
): { status: ScheduleRunStatus | "paused"; count: number } | null {
  if (schedule.disabledReason === "timeout")
    return { status: "timeout", count: 0 };
  if (!schedule.enabled) return { status: "paused", count: 0 };
  if (schedule.running) return { status: "running", count: 0 };
  const run = schedule.lastRun;
  if (run === null) return null;
  return { status: run.status, count: run.newCount };
}

export function Toggle({
  on,
  label,
  onChange,
  disabled = false,
}: {
  on: boolean;
  label: string;
  onChange(on: boolean): void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!on)}
      className={`relative h-[18px] w-8 shrink-0 rounded-full transition-colors disabled:opacity-50 ${on ? "bg-accent" : "bg-[#4a4a46]"}`}
    >
      <span
        className={`absolute top-[2px] h-[14px] w-[14px] rounded-full transition-[left] ${on ? "left-4 bg-white" : "left-[2px] bg-[#cfccc3]"}`}
      />
    </button>
  );
}

// DisabledBanner explains an automatic disable and offers to enable again.
export function DisabledBanner({
  schedule,
  onEnable,
  busy,
}: {
  schedule: Schedule;
  onEnable(): void;
  busy: boolean;
}) {
  const { t, i18n } = useTranslation();
  if (schedule.disabledReason !== "timeout") return null;
  return (
    <div className="flex items-center gap-2.5 rounded-[10px] border border-[#5a3631] bg-[#2c2422] px-3 py-2.5">
      <Icon
        name="alertCircle"
        size="17px"
        className="shrink-0 text-[#ec8f8b]"
      />
      <div className="min-w-0 flex-1 text-[13.5px] text-[#e9ddd9]">
        {t("schedules.disabled.title", {
          date: schedule.disabledAt
            ? formatDay(schedule.disabledAt, i18n.language)
            : "",
        })}
        <div className="text-[12.5px] text-[#a9918c]">
          {t("schedules.disabled.body", { minutes: RUN_MINUTES })}
        </div>
      </div>
      <button
        type="button"
        disabled={busy}
        onClick={onEnable}
        className="ui-control-text shrink-0 rounded-lg bg-[#3a3a37] px-2.5 py-1 text-[13px] text-[#d5d2c9] hover:bg-[#4a4a46] disabled:opacity-50"
      >
        {t("schedules.disabled.enable")}
      </button>
    </div>
  );
}

type PreviewState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "ok"; preview: SchedulePreview }
  | { kind: "error"; code: "too_often" | "not_understood" | "failed" };

const inputClass =
  "w-full rounded-xl border border-[#3f3f3d] bg-[#343433] px-3 py-2 text-[14px] text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f]";

// ScheduleEditDialog creates a schedule (schedule null) or edits one. The
// interval is read as the user types; Save keeps exactly the slot shown.
export function ScheduleEditDialog({
  schedule,
  onClose,
  onSaved,
  onSessionExpired,
}: {
  schedule: Schedule | null;
  onClose(): void;
  onSaved(schedule: Schedule): void;
  onSessionExpired(): void;
}) {
  const { t, i18n } = useTranslation();
  useEscapeKey(onClose);
  const [name, setName] = useState(schedule?.name ?? "");
  const [prompt, setPrompt] = useState(schedule?.prompt ?? "");
  const [interval, setIntervalText] = useState(schedule?.interval ?? "");
  const [preview, setPreview] = useState<PreviewState>({ kind: "idle" });
  const [rerolls, setRerolls] = useState(0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const debounced = useDebouncedValue(interval.trim(), 600);
  const unchanged = schedule !== null && debounced === schedule.interval;

  useEffect(() => {
    if (debounced === "" || (unchanged && rerolls === 0)) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- nothing to read for an empty or unchanged interval
      setPreview({ kind: "idle" });
      return;
    }
    let cancelled = false;
    setPreview({ kind: "loading" });
    previewSchedule(debounced).then(
      (result) => {
        if (!cancelled) setPreview({ kind: "ok", preview: result });
      },
      (err: unknown) => {
        if (cancelled) return;
        if (err instanceof ScheduleIntervalError) {
          setPreview({ kind: "error", code: err.code });
        } else if (describeActionError(err, "") === null) {
          onSessionExpired();
        } else {
          setPreview({ kind: "error", code: "failed" });
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, [debounced, unchanged, rerolls, onSessionExpired]);

  const typing = interval.trim() !== debounced;
  const canSave =
    !saving &&
    !typing &&
    prompt.trim() !== "" &&
    interval.trim() !== "" &&
    (preview.kind === "ok" || (unchanged && preview.kind === "idle"));

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    setError("");
    const slot =
      preview.kind === "ok"
        ? { template: preview.preview.template, cron: preview.preview.cron }
        : {};
    try {
      const input = {
        name: name.trim(),
        prompt: prompt.trim(),
        interval: interval.trim(),
        ...slot,
      };
      const saved =
        schedule === null
          ? await createSchedule(input)
          : await updateSchedule(schedule.id, input);
      onSaved(saved);
    } catch (err) {
      const message = describeActionError(err, t("schedules.form.saveFailed"));
      if (message === null) onSessionExpired();
      else setError(message);
      setSaving(false);
    }
  };

  const shown =
    preview.kind === "ok"
      ? preview.preview
      : unchanged && schedule !== null
        ? {
            cron: schedule.cron,
            summary: schedule.summary,
            timezone: schedule.timezone,
            nextRuns: schedule.nextRuns,
          }
        : null;

  return (
    <div
      className="fixed inset-0 z-40 grid place-items-center bg-[rgba(10,10,9,0.62)] px-4 backdrop-blur-[2px]"
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t(
          schedule === null
            ? "schedules.form.titleNew"
            : "schedules.form.titleEdit",
        )}
        className="max-h-[92svh] w-full max-w-[640px] overflow-y-auto rounded-2xl border border-[#343432] bg-[#262624] shadow-[0_30px_70px_rgba(0,0,0,0.5)]"
      >
        <div className="flex items-center justify-between px-[22px] pb-1 pt-[18px]">
          <h2 className="font-serif text-[22px] font-medium leading-7 text-[#f4f0e8]">
            {t(
              schedule === null
                ? "schedules.form.titleNew"
                : "schedules.form.titleEdit",
            )}
          </h2>
          <button
            type="button"
            aria-label={t("common.close")}
            onClick={onClose}
            className="text-[#a8a59c] hover:text-white"
          >
            <Icon name="close" size="18px" />
          </button>
        </div>
        <div className="grid gap-4 px-[22px] pb-1 pt-3.5">
          {schedule !== null && (
            <DisabledBanner
              schedule={schedule}
              busy={saving}
              onEnable={() => {
                setSaving(true);
                updateSchedule(schedule.id, { enabled: true }).then(
                  onSaved,
                  (err: unknown) => {
                    const message = describeActionError(
                      err,
                      t("schedules.actionFailed"),
                    );
                    if (message === null) onSessionExpired();
                    else setError(message);
                    setSaving(false);
                  },
                );
              }}
            />
          )}
          <label className="block">
            <span className="mb-1.5 block text-[13px] text-[#d5d2c9]">
              {t("schedules.form.name")}{" "}
              <span className="text-[#807d74]">
                · {t("schedules.form.optional")}
              </span>
            </span>
            <input
              className={inputClass}
              value={name}
              maxLength={120}
              onChange={(event) => setName(event.target.value)}
            />
            <span className="mt-1.5 block text-[12px] text-[#807d74]">
              {t("schedules.form.nameHint")}
            </span>
          </label>
          <label className="block">
            <span className="mb-1.5 block text-[13px] text-[#d5d2c9]">
              {t("schedules.form.prompt")}
            </span>
            <textarea
              className={`${inputClass} resize-none leading-normal`}
              rows={4}
              maxLength={4000}
              value={prompt}
              placeholder={t("schedules.form.promptPlaceholder")}
              onChange={(event) => setPrompt(event.target.value)}
            />
          </label>
          <div>
            <label className="block">
              <span className="mb-1.5 block text-[13px] text-[#d5d2c9]">
                {t("schedules.form.when")}
              </span>
              <input
                className={`${inputClass} ${preview.kind === "error" ? "border-[#8a4a42]" : ""}`}
                value={interval}
                maxLength={200}
                placeholder={t("schedules.form.whenPlaceholder")}
                onChange={(event) => setIntervalText(event.target.value)}
              />
            </label>
            {(preview.kind === "loading" ||
              (typing && interval.trim() !== "")) && (
              <div className="mt-2 flex items-center gap-2.5 rounded-[10px] border border-[#3a3a37] bg-[#2d2d2b] px-3 py-2.5 text-[13px] text-[#8f8b82]">
                <span className="inline-block h-[13px] w-[13px] animate-spin rounded-full border-2 border-[#4a4a46] border-t-[#c9c6bd]" />
                {t("schedules.form.interpreting")}
              </div>
            )}
            {!typing && preview.kind === "error" && (
              <div className="mt-2 flex items-start gap-2.5 rounded-[10px] border border-[#5a3631] bg-[#2c2422] px-3 py-2.5">
                <Icon
                  name="alertCircle"
                  size="17px"
                  className="mt-0.5 text-[#e59a90]"
                />
                <div>
                  <div className="text-[14px] text-[#f4f0e8]">
                    {preview.code === "too_often"
                      ? t("schedules.form.tooOften")
                      : preview.code === "not_understood"
                        ? t("schedules.form.notUnderstood")
                        : t("schedules.form.interpretFailed")}
                  </div>
                  {preview.code !== "failed" && (
                    <div className="mt-0.5 text-[12.5px] text-[#9c9a92]">
                      {preview.code === "too_often"
                        ? t("schedules.form.tooOftenHint")
                        : t("schedules.form.notUnderstoodHint")}
                    </div>
                  )}
                </div>
              </div>
            )}
            {!typing && preview.kind !== "loading" && shown !== null && (
              <div className="mt-2 flex items-start gap-2.5 rounded-[10px] border border-[#3a3a37] bg-[#2d2d2b] px-3 py-2.5">
                <Icon
                  name="clock"
                  size="17px"
                  className="mt-0.5 text-accent-strong"
                />
                <div className="min-w-0 flex-1">
                  <div className="text-[14px] text-[#f4f0e8]">
                    {slotText(shown.cron, shown.summary, i18n.language, t)}{" "}
                    <span className="text-[#8f8b82]">· {shown.timezone}</span>
                  </div>
                  {shown.nextRuns.length > 0 && (
                    <div className="mt-0.5 text-[12.5px] text-[#9c9a92]">
                      {t("schedules.form.nextRuns", {
                        runs: shown.nextRuns
                          .map((run) =>
                            formatRunDate(run, i18n.language, shown.timezone),
                          )
                          .join(" · "),
                      })}
                    </div>
                  )}
                  <div className="mt-1 font-mono text-[11.5px] text-[#6f6d66]">
                    {shown.cron}
                  </div>
                </div>
                <button
                  type="button"
                  title={t("schedules.form.rerollTitle")}
                  onClick={() => setRerolls((n) => n + 1)}
                  className="flex shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-1 text-[13px] text-[#d5d2c9] hover:bg-[#4a4a46]"
                >
                  <Icon name="retry" size="14px" />
                  {t("schedules.form.reroll")}
                </button>
              </div>
            )}
            <div className="mt-1.5 text-[12px] text-[#807d74]">
              {t("schedules.form.whenHint")}
            </div>
          </div>
          {error !== "" && <ErrorText>{error}</ErrorText>}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 px-[22px] pb-[18px] pt-4">
          <div className="flex items-center gap-2 text-[12.5px] text-[#8f8b82]">
            <Icon name="lock" size="14px" />
            {t("schedules.form.limits", {
              tokens: formatTokens(RUN_TOKEN_LIMIT),
              minutes: RUN_MINUTES,
            })}
          </div>
          <div className="flex gap-2">
            <button
              type="button"
              className={modalCancelButtonClass}
              onClick={onClose}
            >
              {t("common.cancel")}
            </button>
            <button
              type="button"
              disabled={!canSave}
              onClick={() => void save()}
              className="ui-control-text rounded-lg bg-white px-3 py-1.5 font-medium text-[#1d1d1b] disabled:opacity-50"
            >
              {t("common.save")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
