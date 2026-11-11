import { useId } from "react";
import { useTranslation } from "react-i18next";

import type { TodayRun } from "../api/today";
import { Icon } from "../chat/Icon";
import { formatTimeAgo } from "../timeago";

// ScheduleNews lists the unread schedule runs with the model's summary, each
// to open in its thread or to mark read on the spot.
export function ScheduleNews({
  runs,
  onOpenThread,
  onMarkRead,
  onMarkAllRead,
}: {
  runs: TodayRun[];
  onOpenThread(threadID: string): void;
  onMarkRead(id: string): void;
  onMarkAllRead(): void;
}) {
  const { t } = useTranslation();
  const headingId = useId();
  if (runs.length === 0) return null;
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-2.5">
      <div className="flex items-center justify-between gap-3">
        <h2
          // pl-2.5: flush with the weather heading and the project picker.
          className="flex items-center gap-2 pl-2.5 text-[13px] font-medium tracking-[0.02em] text-muted"
          id={headingId}
        >
          {t("today.news.title")}
          <span
            aria-hidden
            className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-unread/16 px-1.5 text-xs text-unread-ink"
            data-testid="news-count"
          >
            {runs.length}
          </span>
          <span className="sr-only">
            {t("today.news.unread", { count: runs.length })}
          </span>
        </h2>
        <button
          className="ui-control-text h-11 rounded-lg px-2.5 text-[13px] text-ink-dim transition-colors hover:bg-raised sm:h-[30px]"
          onClick={onMarkAllRead}
          type="button"
        >
          {t("today.news.markAllRead")}
        </button>
      </div>
      <ul className="flex flex-col gap-2.5">
        {runs.map((run) => (
          <li key={run.id}>
            <article className="flex items-start gap-3.5 rounded-2xl border border-border bg-card py-3.5 pr-1.5 pl-[18px] sm:pr-3.5">
              <span
                aria-hidden
                className="mt-2 h-2 w-2 shrink-0 rounded-full bg-unread"
              />
              {/* The title's button stretches over the whole text: a click
                  on the title, the time or the summary opens the thread. */}
              <div className="group relative flex min-w-0 flex-1 flex-col gap-1">
                <div className="flex flex-wrap items-baseline gap-x-2.5">
                  <h3 className="text-[15px] font-medium text-ink">
                    {run.threadId !== null ? (
                      <button
                        className="cursor-pointer text-left group-hover:underline after:absolute after:inset-0"
                        onClick={() => onOpenThread(run.threadId as string)}
                        type="button"
                      >
                        {run.title}
                      </button>
                    ) : (
                      run.title
                    )}
                  </h3>
                  <span className="text-xs text-muted">
                    {formatTimeAgo(run.createdAt)}
                  </span>
                </div>
                {run.body !== "" && (
                  <p className="text-sm leading-normal text-ink-dim">
                    {run.body}
                  </p>
                )}
              </div>
              <button
                aria-label={t("today.news.markRead")}
                className="grid h-11 w-11 shrink-0 place-items-center rounded-[10px] text-muted transition-colors hover:bg-raised hover:text-ink-bright"
                onClick={() => onMarkRead(run.id)}
                title={t("today.news.markRead")}
                type="button"
              >
                <Icon name="check" size="18px" />
              </button>
            </article>
          </li>
        ))}
      </ul>
    </section>
  );
}
