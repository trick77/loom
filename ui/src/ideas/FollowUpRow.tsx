import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { Icon } from "../chat/Icon";
import { useEscapeKey } from "../chat/useEscapeKey";
import { useOutsideRefPointerDown } from "../chat/useOutsidePointerDown";
import { menuIconClass, menuItemClass } from "../ThreadActionsMenu";
import { addDays, daysBetween, formatDay, formatWeekday } from "./ideaText";

export type IdeaDates = { dueOn: string | null; remindOn: string | null };

// The follow-up offsets the chip's menu offers before a due date.
const BEFORE_DAYS = [1, 7, 14] as const;

// beforeLabel names how far a follow-up lies before its due date: "1 week
// before", "3 days before", "on the day".
function beforeLabel(
  t: ReturnType<typeof useTranslation>["t"],
  days: number,
): string {
  if (days === 0) return t("ideas.followUp.onDay");
  if (days % 7 === 0)
    return t("ideas.followUp.weeksBefore", { count: days / 7 });
  return t("ideas.followUp.daysBefore", { count: days });
}

/**
 * FollowUpRow sets an idea's follow-up: "Set follow-up" picks a date; with a
 * due date it reads "Due 12 Jan" and a chip "Follow-up Mon 5 Jan · 1 week
 * before" whose menu offers 1 day, 1 week, 2 weeks before or a picked date,
 * and × drops it. Every date is one the server accepts: not before today,
 * a follow-up not after the due date.
 */
export function FollowUpRow({
  value,
  today,
  onChange,
}: {
  value: IdeaDates;
  /** The user's 'YYYY-MM-DD'. */
  today: string;
  onChange(next: IdeaDates): void;
}) {
  const { t, i18n } = useTranslation();
  const [picking, setPicking] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  useOutsideRefPointerDown(menuOpen, menuRef, () => setMenuOpen(false));
  useEscapeKey(() => setMenuOpen(false), { active: menuOpen });
  useEscapeKey(() => setPicking(false), { active: picking });
  const { dueOn, remindOn } = value;
  const language = i18n.language;
  // A due date that already passed takes no follow-up before it.
  const dueAhead = dueOn !== null && dueOn >= today ? dueOn : null;
  const setRemind = (day: string | null) => {
    setMenuOpen(false);
    setPicking(false);
    onChange({ dueOn, remindOn: day });
  };
  // A picked date the server would refuse (past, after the due date) or an
  // empty field just closes the picker.
  const commitPicked = (day: string) => {
    if (day !== "" && day >= today && (dueAhead === null || day <= dueAhead))
      setRemind(day);
    else setPicking(false);
  };
  const offsets =
    dueAhead === null
      ? []
      : BEFORE_DAYS.filter((days) => addDays(dueAhead, -days) >= today);

  const chipText =
    remindOn === null
      ? ""
      : [
          t("ideas.followUp.chip", { date: formatWeekday(remindOn, language) }),
          ...(dueOn !== null && remindOn <= dueOn
            ? [beforeLabel(t, daysBetween(remindOn, dueOn))]
            : []),
        ].join(" · ");

  return (
    <div
      className="mt-3 flex min-h-7 flex-wrap items-center gap-x-2 gap-y-1.5 text-[13px] text-[#a8a59c]"
      data-testid="follow-up-row"
    >
      <Icon name="clock" size="15px" className="shrink-0 text-[#8f8b82]" />
      {dueOn !== null && (
        <span className="inline-flex items-center gap-0.5">
          {t("ideas.dueOnly", { date: formatDay(dueOn, language) })}
          <button
            type="button"
            aria-label={t("ideas.followUp.clearDue")}
            title={t("ideas.followUp.clearDue")}
            className="grid h-6 w-6 place-items-center rounded-md text-[#8f8b82] hover:text-white"
            onClick={() => onChange({ dueOn: null, remindOn })}
          >
            <Icon name="close" size="12px" />
          </button>
        </span>
      )}
      {picking ? (
        <input
          type="date"
          autoFocus
          aria-label={t("ideas.followUp.date")}
          min={today}
          max={dueAhead ?? undefined}
          defaultValue={remindOn ?? ""}
          className="h-7 rounded-lg border border-[#3f3f3d] bg-[#262624] px-2 text-[13px] text-ink [color-scheme:dark]"
          // Taken on Enter or when the field is left, never per keystroke:
          // typing "15" passes through the 1st on its way.
          onKeyDown={(event) => {
            if (event.key !== "Enter") return;
            // Not the form's submit.
            event.preventDefault();
            commitPicked(event.currentTarget.value);
          }}
          onBlur={(event) => commitPicked(event.currentTarget.value)}
        />
      ) : (
        <div ref={menuRef} className="relative inline-flex">
          {remindOn !== null ? (
            <span className="inline-flex h-7 items-center gap-0.5 rounded-lg bg-unread/16 pl-2.5 pr-0.5 text-unread-ink">
              <button
                type="button"
                aria-haspopup="menu"
                aria-expanded={menuOpen}
                aria-label={`${chipText}. ${t("ideas.followUp.change")}`}
                className="inline-flex items-center gap-1.5"
                onClick={() => setMenuOpen((open) => !open)}
              >
                {chipText}
                <Icon name="chevronDown" size="12px" className="opacity-70" />
              </button>
              <button
                type="button"
                aria-label={t("ideas.followUp.clear")}
                title={t("ideas.followUp.clear")}
                className="grid h-6 w-6 place-items-center rounded-md opacity-70 hover:opacity-100"
                onClick={() => setRemind(null)}
              >
                <Icon name="close" size="12px" />
              </button>
            </span>
          ) : dueOn !== null && dueAhead === null ? (
            // The due date passed: no follow-up can lie before it (the
            // server refuses one after it). Dropping the date frees the row.
            <span className="text-[#807d74]">{t("ideas.duePassed")}</span>
          ) : (
            <button
              type="button"
              aria-haspopup={offsets.length > 0 ? "menu" : undefined}
              aria-expanded={offsets.length > 0 ? menuOpen : undefined}
              className="h-7 text-[#8f8b82] hover:text-white"
              onClick={() =>
                offsets.length > 0
                  ? setMenuOpen((open) => !open)
                  : setPicking(true)
              }
            >
              {t("ideas.followUp.set")}
            </button>
          )}
          {menuOpen && (
            <div
              role="menu"
              className="absolute left-0 top-full z-40 mt-1 w-[200px] rounded-[10px] border border-[#454540] bg-[#363632] p-1 shadow-[0_18px_32px_rgba(0,0,0,0.38)]"
            >
              {dueAhead !== null &&
                offsets.map((days) => (
                  <button
                    key={days}
                    type="button"
                    role="menuitem"
                    className={`${menuItemClass} text-ink-bright`}
                    onClick={() => setRemind(addDays(dueAhead, -days))}
                  >
                    <Icon
                      name={
                        remindOn === addDays(dueAhead, -days)
                          ? "check"
                          : "clock"
                      }
                      size="16px"
                      className={menuIconClass}
                    />
                    {beforeLabel(t, days)}
                  </button>
                ))}
              <button
                type="button"
                role="menuitem"
                className={`${menuItemClass} text-ink-bright`}
                onClick={() => {
                  setMenuOpen(false);
                  setPicking(true);
                }}
              >
                <Icon name="edit" size="16px" className={menuIconClass} />
                {t("ideas.followUp.pick")}
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
