import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { createIdea, updateIdea, type Idea } from "../api";
import { describeActionError } from "../chat/actionErrors";
import { ErrorText } from "../chat/ErrorText";
import { Icon } from "../chat/Icon";
import { useEscapeKey } from "../chat/useEscapeKey";
import { modalCancelButtonClass } from "../ThreadActionsMenu";
import { FollowUpRow, type IdeaDates } from "./FollowUpRow";
import {
  defaultRemindOn,
  EXCERPT_MAX,
  REASON_MAX,
  TITLE_MAX,
  todayDay,
} from "./ideaText";
import { useIdeaDraft } from "./useIdeaDraft";

// 16px on touch: iOS zooms the page into a smaller input.
export const ideaInputClass =
  "w-full rounded-[10px] border border-[#3f3f3d] bg-[#262624] px-2.5 py-2 text-base text-ink outline-none placeholder:text-[#807d74] focus:border-[#69665f] md:text-[15px]";

/** A hover device gets the reason focused; a touch one keeps its keyboard down. */
export function canHover(): boolean {
  return (
    typeof window.matchMedia === "function" &&
    window.matchMedia("(hover: hover)").matches
  );
}

// IdeaEditDialog creates an idea (idea null) or edits one: title, the
// required reason and an optional passage.
export function IdeaEditDialog({
  idea,
  onClose,
  onSaved,
  onSessionExpired,
}: {
  idea: Idea | null;
  onClose(): void;
  onSaved(idea: Idea): void;
  onSessionExpired(): void;
}) {
  const { t } = useTranslation();
  const [title, setTitle] = useState(idea?.title ?? "");
  const [reason, setReason] = useState(idea?.reason ?? "");
  const [excerpt, setExcerpt] = useState(idea?.excerpt ?? "");
  const [dates, setDates] = useState<IdeaDates>({
    dueOn: idea?.dueOn ?? null,
    remindOn: idea?.remindOn ?? null,
  });
  const [datesEdited, setDatesEdited] = useState(false);
  // Read by a late answer: a call already running when the user set or
  // dropped a date must not move it back.
  const datesEditedRef = useRef(false);
  const [today] = useState(() => todayDay());
  // A new idea's reason is asked for the due date it names, as in the
  // capture; its title and passage are the user's to type, and the passage
  // never goes to the model. An edit keeps its dates as they are.
  useIdeaDraft(
    "",
    reason,
    { enabled: idea === null && !datesEdited },
    (draft) => {
      if (draft.dueOn !== null && !datesEditedRef.current)
        setDates({
          dueOn: draft.dueOn,
          remindOn: defaultRemindOn(draft.dueOn, today),
        });
    },
  );
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const pressedBackdrop = useRef(false);
  useEscapeKey(onClose);
  const heading = t(
    idea === null ? "ideas.form.titleNew" : "ideas.form.titleEdit",
  );
  const canSave = reason.trim() !== "" && !saving;

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    setError("");
    try {
      // A new idea sends both dates: the follow-up shown is the one saved.
      // An edit sends them only when changed here, so a stale dialog never
      // moves back a date set elsewhere meanwhile (and with it a sent
      // follow-up that would go out again).
      const changed =
        idea === null ||
        dates.dueOn !== idea.dueOn ||
        dates.remindOn !== idea.remindOn;
      const input = { title, reason, excerpt, ...(changed ? dates : {}) };
      onSaved(
        idea === null
          ? await createIdea(input)
          : await updateIdea(idea.id, input),
      );
    } catch (err) {
      const message = describeActionError(err, t("ideas.actionFailed"));
      if (message === null) onSessionExpired();
      else setError(message);
      setSaving(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-40 grid place-items-center bg-[rgba(10,10,9,0.62)] px-4 backdrop-blur-[2px]"
      // Closes only on a click that also started on the backdrop: a text
      // selection dragged out of a field must not throw the draft away.
      onPointerDown={(event) => {
        pressedBackdrop.current = event.target === event.currentTarget;
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget && pressedBackdrop.current)
          onClose();
        pressedBackdrop.current = false;
      }}
    >
      <form
        role="dialog"
        aria-modal="true"
        aria-label={heading}
        className="max-h-[92svh] w-full max-w-[520px] overflow-y-auto rounded-2xl border border-[#343432] bg-popover p-[18px] shadow-[0_30px_70px_rgba(0,0,0,0.5)]"
        onSubmit={(event) => {
          event.preventDefault();
          void save();
        }}
      >
        <div className="flex items-center justify-between">
          <h2 className="flex items-center gap-2 font-serif text-[22px] font-medium leading-7 text-[#f4f0e8]">
            <Icon name="bulb" size="20px" className="text-unread-ink" />
            {heading}
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
        <label className="mt-4 block">
          <span className="mb-1.5 flex justify-between text-[12px] text-[#a8a59c]">
            {t("ideas.form.reason")}
            <span className="text-[#807d74]">{t("ideas.form.required")}</span>
          </span>
          <textarea
            autoFocus={canHover()}
            className={`${ideaInputClass} resize-none font-serif font-[360] leading-[22px]`}
            rows={3}
            maxLength={REASON_MAX}
            value={reason}
            placeholder={t("ideas.form.reasonPlaceholder")}
            onChange={(event) => setReason(event.target.value)}
          />
        </label>
        <FollowUpRow
          value={dates}
          today={today}
          onChange={(next) => {
            setDates(next);
            setDatesEdited(true);
            datesEditedRef.current = true;
          }}
        />
        <label className="mt-3 block">
          <span className="mb-1.5 block text-[12px] text-[#a8a59c]">
            {t("ideas.form.title")}
          </span>
          <input
            className={ideaInputClass}
            maxLength={TITLE_MAX}
            value={title}
            onChange={(event) => setTitle(event.target.value)}
          />
        </label>
        <label className="mt-3 block">
          <span className="mb-1.5 flex justify-between text-[12px] text-[#a8a59c]">
            {t("ideas.form.excerpt")}
            <span className="text-[#807d74]">{t("ideas.form.optional")}</span>
          </span>
          <textarea
            className={`${ideaInputClass} resize-none font-serif font-[360] leading-[22px]`}
            rows={3}
            maxLength={EXCERPT_MAX}
            value={excerpt}
            onChange={(event) => setExcerpt(event.target.value)}
          />
        </label>
        {error !== "" && <ErrorText>{error}</ErrorText>}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className={modalCancelButtonClass}
            onClick={onClose}
          >
            {t("common.cancel")}
          </button>
          <button
            type="submit"
            disabled={!canSave}
            className="ui-control-text rounded-lg bg-white px-3 py-1.5 font-medium text-[#1d1d1b] disabled:bg-[#282827] disabled:text-[#8c8a82]"
          >
            {t("common.save")}
          </button>
        </div>
      </form>
    </div>
  );
}
