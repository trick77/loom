import { useId } from "react";
import { useTranslation } from "react-i18next";

import type { Idea } from "../api";
import { Icon } from "../chat/Icon";
import { daysBetween, formatWeekday, todayDay } from "../ideas/ideaText";

const buttonBase =
  "inline-flex h-11 items-center gap-1.5 whitespace-nowrap rounded-lg px-3 text-[14px] font-medium leading-5 transition-colors sm:h-8";

// IdeaFollowUps lists the ideas due for a follow-up (the server's at most
// three): what it is, when it is due and why it was kept, with a thread to
// start about it, the idea to open, a hide for this follow-up and a check
// that marks it done. Every text is the user's own, shown as plain text.
export function IdeaFollowUps({
  ideas,
  onStartThread,
  onOpenIdea,
  onHide,
  onDone,
}: {
  ideas: Idea[];
  onStartThread(idea: Idea): void;
  onOpenIdea(ideaID: string): void;
  onHide(ideaID: string): void;
  onDone(ideaID: string): void;
}) {
  const { t, i18n } = useTranslation();
  const headingId = useId();
  if (ideas.length === 0) return null;
  const today = todayDay();
  const when = (idea: Idea) => {
    if (idea.dueOn === null)
      return idea.remindOn === null
        ? ""
        : t("ideas.followUpOnly", {
            date: formatWeekday(idea.remindOn, i18n.language),
          });
    const date = formatWeekday(idea.dueOn, i18n.language);
    const days = daysBetween(today, idea.dueOn);
    return days <= 0
      ? t("today.ideas.dueToday", { date })
      : t("today.ideas.dueIn", { count: days, date });
  };
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-2.5">
      <h2
        // pl-2.5: flush with the weather heading and the project picker.
        className="flex min-h-[30px] items-center gap-2 pl-2.5 text-[13px] font-medium tracking-[0.02em] text-muted"
        id={headingId}
      >
        {t("today.ideas.title")}
        <span
          aria-hidden
          className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-unread/16 px-1.5 text-xs text-unread-ink"
          data-testid="follow-up-count"
        >
          {ideas.length}
        </span>
        <span className="sr-only">
          {t("today.ideas.count", { count: ideas.length })}
        </span>
      </h2>
      <ul className="flex flex-col gap-2.5">
        {ideas.map((idea) => (
          <li key={idea.id}>
            <article className="flex items-start gap-3.5 rounded-2xl border border-border bg-card py-3.5 pr-1.5 pl-[18px] sm:pr-3.5">
              <Icon
                name="bulb"
                size="18px"
                className="mt-0.5 shrink-0 text-unread-ink"
              />
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <div className="flex flex-wrap items-baseline gap-x-2.5">
                  <h3 className="text-[15px] font-medium text-ink">
                    {idea.title}
                  </h3>
                  {when(idea) !== "" && (
                    <span className="text-xs text-unread-ink">
                      {when(idea)}
                    </span>
                  )}
                </div>
                <p className="text-sm leading-normal text-ink-dim">
                  {idea.reason}
                </p>
                <div className="mt-2 flex flex-wrap gap-2">
                  <button
                    className={`${buttonBase} bg-white text-[#1d1d1b] hover:bg-[#ece9e2]`}
                    onClick={() => onStartThread(idea)}
                    type="button"
                  >
                    <Icon name="messages" size="16px" />
                    {t("ideas.startThread")}
                  </button>
                  <button
                    className={`${buttonBase} bg-[#343433] text-[#f5f3ee] hover:bg-[#3d3d3b]`}
                    onClick={() => onOpenIdea(idea.id)}
                    type="button"
                  >
                    {t("today.ideas.open")}
                  </button>
                  <button
                    className={`${buttonBase} text-ink-dim hover:text-ink-bright`}
                    onClick={() => onHide(idea.id)}
                    type="button"
                  >
                    {t("today.ideas.hide")}
                  </button>
                </div>
              </div>
              <button
                aria-label={t("today.ideas.done")}
                className="grid h-11 w-11 shrink-0 place-items-center rounded-[10px] text-muted transition-colors hover:bg-raised hover:text-ink-bright"
                onClick={() => onDone(idea.id)}
                title={t("today.ideas.done")}
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
