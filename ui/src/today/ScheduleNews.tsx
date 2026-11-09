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
          className="flex items-center gap-2 text-[13px] font-medium tracking-[0.02em] text-[#97958c]"
          id={headingId}
        >
          {t("today.news.title")}
          <span
            aria-hidden
            className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-[rgba(201,162,95,0.16)] px-1.5 text-xs text-[#e0c48f]"
            data-testid="news-count"
          >
            {runs.length}
          </span>
          <span className="sr-only">
            {t("today.news.unread", { count: runs.length })}
          </span>
        </h2>
        <button
          className="ui-control-text h-11 rounded-lg px-2.5 text-[13px] text-[#c7c5bd] transition-colors hover:bg-[#2a2a28] sm:h-[30px]"
          onClick={onMarkAllRead}
          type="button"
        >
          {t("today.news.markAllRead")}
        </button>
      </div>
      <ul className="flex flex-col gap-2.5">
        {runs.map((run) => (
          <li key={run.id}>
            <article className="flex items-start gap-3.5 rounded-2xl border border-[#323230] bg-[#242422] py-3.5 pr-1.5 pl-[18px] sm:pr-3.5">
              <span
                aria-hidden
                className="mt-2 h-2 w-2 shrink-0 rounded-full bg-[#c9a25f]"
              />
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <div className="flex flex-wrap items-baseline gap-x-2.5">
                  <h3 className="text-[15px] font-medium text-[#faf9f5]">
                    {run.title}
                  </h3>
                  <span className="text-xs text-[#8f8b82]">
                    {formatTimeAgo(run.createdAt)}
                  </span>
                </div>
                {run.body !== "" && (
                  <p className="text-sm leading-normal text-[#c7c5bd]">
                    {run.body}
                  </p>
                )}
                {run.threadId !== null && (
                  <button
                    className="mt-1 self-start text-[13px] font-medium text-[#e8e6dc] hover:text-white hover:underline"
                    onClick={() => onOpenThread(run.threadId as string)}
                    type="button"
                  >
                    {t("today.news.open")}
                  </button>
                )}
              </div>
              <button
                aria-label={t("today.news.markRead")}
                className="grid h-11 w-11 shrink-0 place-items-center rounded-[10px] text-[#97958c] transition-colors hover:bg-[#2a2a28] hover:text-[#f3f0e8]"
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
